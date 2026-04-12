package news

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/observability"
	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/newsdata"
	"ai-trading-agents/internal/platform"
)

type contextKey string

const agentIDContextKey contextKey = "agent_id"

// Service implements the News MCP - headlines, sentiment, and events for trading agents.
type Service struct {
	svcCtx *platform.ServiceContext
	log    *zap.Logger
	source newsdata.Source
	repo   db.Repository // nil = no keyword alert support

	alertPollInterval time.Duration
}

// NewService creates the News MCP service.
func NewService(svcCtx *platform.ServiceContext, source newsdata.Source, repo db.Repository) *Service {
	return &Service{
		svcCtx:            svcCtx,
		log:               svcCtx.Logger().Named("news_mcp"),
		source:            source,
		repo:              repo,
		alertPollInterval: 5 * time.Minute, // news checked less frequently
	}
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("news-mcp", "1.0.0", s.tools(), s.handleTool)
	s.svcCtx.Router().Post("/mcp/news", func(w http.ResponseWriter, r *http.Request) {
		if agentID := r.Header.Get("X-Agent-ID"); agentID != "" {
			ctx := context.WithValue(r.Context(), agentIDContextKey, agentID)
			ctx = observability.WithLogger(ctx, s.log.With(zap.String("agent_id", agentID)))
			r = r.WithContext(ctx)
		}
		mcpServer.ServeHTTP(w, r)
	})
	s.log.Info("News MCP registered at /mcp/news", zap.String("source", s.source.Name()))
	return nil
}

func (s *Service) Start() error {
	if s.repo != nil {
		go s.runKeywordAlertPoller()
	}
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) Stop() error {
	s.log.Info("News MCP stopped")
	return nil
}

func (s *Service) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "news/search",
			Description: "Search crypto news by category and market filters. " +
				"Returns: {articles: [{title, source, url, published_at, summary, sentiment, markets, kind}], count, source}. " +
				"sentiment: \"positive\", \"negative\", or \"neutral\" based on community votes. " +
				"Use 'markets' for ticker symbols (BTC, ETH) and 'categories' for topic tags (REGULATION, TRADING). " +
				"Use filter param for curated feeds: \"rising\" (gaining traction), \"hot\" (trending), " +
				"\"bullish\", \"bearish\", \"important\" (editor picks).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"markets": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by crypto ticker symbols: BTC, ETH, SOL, XRP, ADA, DOGE, DOT, AVAX, LINK, MATIC, UNI, AAVE, LTC, BNB, TRX, TON, USDT, USDC, ATOM, ARB, and more.",
					},
					"categories": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string", "enum": []string{"REGULATION", "TRADING", "MARKET", "EXCHANGE", "BUSINESS", "TECHNOLOGY", "BLOCKCHAIN", "MACROECONOMICS", "FIAT", "ICO", "TOKEN SALE", "MINING", "SPONSORED", "OTHER"}},
						"description": "Filter by topic category. Combine with 'markets' for precise results. Example: categories=[\"REGULATION\"] + markets=[\"BTC\"] returns BTC regulation news.",
					},
					"filter": map[string]any{
						"type":        "string",
						"enum":        []string{"rising", "hot", "bullish", "bearish", "important"},
						"description": "Pre-curated feed filter. \"important\" = editor picks, \"hot\" = trending, \"bullish\"/\"bearish\" = community sentiment vote.",
					},
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"news", "media", "all"},
						"description": "Content type filter (default: all)",
					},
					"date_from": map[string]any{
						"type":        "string",
						"description": "RFC3339 datetime (e.g. 2026-03-09T00:00:00Z) - only articles published after this time",
					},
					"date_to": map[string]any{
						"type":        "string",
						"description": "RFC3339 datetime (e.g. 2026-03-09T23:59:59Z) - only articles published before this time",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max articles to return (default 10, max 50)",
					},
				},
			},
		},
		{
			Name: "news/get_latest",
			Description: "Get latest crypto news headlines. " +
				"Returns: {articles: [{title, source, url, published_at, sentiment, markets, kind}], count, source}. " +
				"Use kind to filter content type: \"news\", \"media\", or \"all\". " +
				"Use filter for curated feeds: \"hot\", \"rising\", \"important\", \"bullish\", \"bearish\".",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"news", "media", "all"},
						"description": "Content type filter (default: all)",
					},
					"markets": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by crypto symbols, e.g. [\"BTC\", \"ETH\"]",
					},
					"filter": map[string]any{
						"type":        "string",
						"enum":        []string{"rising", "hot", "bullish", "bearish", "important"},
						"description": "Pre-curated feed filter",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max articles to return (default 10, max 50)",
					},
				},
			},
		},
		{
			Name: "news/get_sentiment",
			Description: "Get aggregated news sentiment for a crypto asset or topic. " +
				"Returns: {query, sentiment, score, article_count, key_themes, period, source}. " +
				"score: -1.0 (very bearish) to 1.0 (very bullish), derived from community vote ratios. " +
				"key_themes: top mentioned assets in the articles. " +
				"period: time window analyzed (\"1h\", \"4h\", \"24h\", \"7d\").",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Asset or topic to analyze sentiment for (e.g. \"ETH\", \"Bitcoin\", \"DeFi\")",
					},
					"markets": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by crypto symbols for more precise results",
					},
					"period": map[string]any{
						"type":        "string",
						"enum":        []string{"1h", "4h", "24h", "7d"},
						"description": "Time window for sentiment analysis (default: 24h)",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "news/set_alert",
			Description: "Set a news alert that fires when articles matching the filters appear. " +
				"The poller checks news every 5 minutes. When triggered, the agent is woken up with the alert context. " +
				"At least one of 'markets' or 'categories' is required. " +
				"Returns: {alert_id, markets, categories, title_contains, on_trigger, status}.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"markets": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Ticker symbols to monitor, e.g. [\"BTC\", \"ETH\"]",
					},
					"categories": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string", "enum": []string{"REGULATION", "TRADING", "MARKET", "EXCHANGE", "BUSINESS", "TECHNOLOGY", "BLOCKCHAIN", "MACROECONOMICS", "FIAT", "ICO", "TOKEN SALE", "MINING", "SPONSORED", "OTHER"}},
						"description": "Topic categories to monitor, e.g. [\"REGULATION\", \"MARKET\"]",
					},
					"title_contains": map[string]any{
						"type":        "string",
						"description": "Optional: only trigger if article title contains this text (case-insensitive). Client-side filter applied after API results.",
					},
					"on_trigger": map[string]any{
						"type":        "string",
						"enum":        []string{"wake_full", "wake_triage"},
						"description": "Action on match: wake_full = start full session, wake_triage = quick Haiku triage first (default: wake_full)",
					},
					"note": map[string]any{
						"type":        "string",
						"description": "Optional note about why this alert was set",
					},
					"max_triggers": map[string]any{
						"type":        "integer",
						"description": "Max times this alert can fire before expiring (default 1, 0 = unlimited)",
					},
				},
			},
		},
		{
			Name:        "news/get_triggered_alerts",
			Description: "Fetch and acknowledge triggered news alerts for this agent. " +
				"Returns: {alerts: [{alert_id, markets, categories, title_contains, note, triggered_at}]}.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "news/get_events",
			Description: "Get market-moving crypto events (upcoming and recent) classified from important news. " +
				"Returns: {events: [{title, type, date, impact_level, details, market}], count, source}. " +
				"Event types: \"fork\", \"upgrade\", \"regulation\", \"hack\", \"listing\", \"unlock\", \"macro\". " +
				"impact_level: \"high\", \"medium\", \"low\". " +
				"Events are extracted from important news articles published within the lookback window. " +
				"Articles about upcoming events (e.g. scheduled forks, upgrades) will appear here - read titles for event timing.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"markets": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter events by crypto symbols, e.g. [\"ETH\", \"BTC\"]",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"fork", "upgrade", "regulation", "hack", "listing", "unlock", "macro"},
						"description": "Filter by event type",
					},
					"days": map[string]any{
						"type":        "integer",
						"description": "How many days of recent news to scan for events (default 7, max 30)",
					},
				},
			},
		},
	}
}

func (s *Service) handleTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	switch toolName {
	case "news/search":
		return s.toolSearch(ctx, args)
	case "news/get_latest":
		return s.toolGetLatest(ctx, args)
	case "news/get_sentiment":
		return s.toolGetSentiment(ctx, args)
	case "news/get_events":
		return s.toolGetEvents(ctx, args)
	case "news/set_alert":
		return s.toolSetAlert(ctx, args)
	case "news/get_triggered_alerts":
		return s.toolGetNewsTriggeredAlerts(ctx)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (s *Service) toolSearch(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	params := newsdata.SearchParams{
		Markets:    parseStringArray(args, "markets"),
		Categories: parseStringArray(args, "categories"),
		Filter:     optString(args, "filter"),
		Kind:       optString(args, "kind"),
		Limit:      optInt(args, "limit", 10),
	}

	if df, err := parseOptionalTime(args, "date_from"); err != nil {
		return nil, fmt.Errorf("invalid date_from: %w", err)
	} else {
		params.DateFrom = df
	}
	if dt, err := parseOptionalTime(args, "date_to"); err != nil {
		return nil, fmt.Errorf("invalid date_to: %w", err)
	} else {
		params.DateTo = dt
	}

	articles, err := s.source.Search(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"articles": articles,
		"count":    len(articles),
		"source":   s.source.Name(),
	})
}

func (s *Service) toolGetLatest(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	params := newsdata.LatestParams{
		Kind:    optString(args, "kind"),
		Markets: parseStringArray(args, "markets"),
		Filter:  optString(args, "filter"),
		Limit:   optInt(args, "limit", 10),
	}

	articles, err := s.source.GetLatest(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("get latest: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"articles": articles,
		"count":    len(articles),
		"source":   s.source.Name(),
	})
}

func (s *Service) toolGetSentiment(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	params := newsdata.SentimentParams{
		Query:   query,
		Markets: parseStringArray(args, "markets"),
		Period:  optString(args, "period"),
	}
	if params.Period == "" {
		params.Period = "24h"
	}

	result, err := s.source.GetSentiment(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("get sentiment: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"query":         result.Query,
		"sentiment":     result.Sentiment,
		"score":         result.Score,
		"article_count": result.ArticleCount,
		"key_themes":    result.KeyThemes,
		"period":        result.Period,
		"source":        s.source.Name(),
	})
}

func (s *Service) toolGetEvents(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	markets := parseStringArray(args, "markets")
	eventType := optString(args, "type")
	days := optInt(args, "days", 7)
	if days > 30 {
		days = 30
	}

	// Events are derived from important/hot news - use the source to search
	params := newsdata.SearchParams{
		Markets: markets,
		Filter:  "important",
		Kind:    "news",
		Limit:   50, // fetch more, then filter/classify
	}

	articles, err := s.source.Search(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}

	events := articlesToEvents(articles, eventType, days)

	return mcp.JSONResult(map[string]any{
		"events": events,
		"count":  len(events),
		"source": s.source.Name(),
	})
}

// articlesToEvents converts important news articles into structured events.
func articlesToEvents(articles []newsdata.Article, eventType string, days int) []newsdata.Event {
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	var events []newsdata.Event

	for _, a := range articles {
		// Only include articles published within the lookback window
		if a.PublishedAt.IsZero() || a.PublishedAt.Before(cutoff) {
			continue
		}

		evt := classifyArticleAsEvent(a)
		if evt == nil {
			continue
		}

		// Filter by event type if specified
		if eventType != "" && evt.Type != eventType {
			continue
		}

		events = append(events, *evt)
	}

	return events
}

// classifyArticleAsEvent attempts to classify a news article as a market event.
func classifyArticleAsEvent(a newsdata.Article) *newsdata.Event {
	titleLower := strings.ToLower(a.Title)

	var eventType, impactLevel string

	// Classify based on title keywords
	switch {
	case containsAny(titleLower, "hack", "exploit", "breach", "stolen", "attack", "vulnerability"):
		eventType, impactLevel = "hack", "high"
	case containsAny(titleLower, "upgrade", "hard fork", "soft fork", "pectra", "dencun", "cancun"):
		eventType, impactLevel = "upgrade", "high"
	case containsAny(titleLower, "fork"):
		eventType, impactLevel = "fork", "high"
	case containsAny(titleLower, "sec ", "regulation", "regulatory", "ban", "legal", "lawsuit", "court"):
		eventType, impactLevel = "regulation", "high"
	case containsAny(titleLower, "listing", "listed", "delist", "launch"):
		eventType, impactLevel = "listing", "medium"
	case containsAny(titleLower, "unlock", "vesting", "release", "airdrop"):
		eventType, impactLevel = "unlock", "medium"
	case containsAny(titleLower, "fed ", "fomc", "cpi", "inflation", "interest rate", "gdp", "employment"):
		eventType, impactLevel = "macro", "high"
	default:
		return nil
	}

	market := ""
	if len(a.Markets) > 0 {
		market = a.Markets[0]
	}

	date := ""
	if !a.PublishedAt.IsZero() {
		date = a.PublishedAt.Format(time.RFC3339)
	}

	return &newsdata.Event{
		Title:       a.Title,
		Type:        eventType,
		Date:        date,
		ImpactLevel: impactLevel,
		Details:     fmt.Sprintf("Source: %s", a.Source),
		Market:      market,
	}
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// Helper functions for parsing tool arguments.

func parseStringArray(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

func optString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func optInt(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key].(float64); ok && v > 0 {
		return int(v)
	}
	return defaultVal
}

func parseOptionalTime(args map[string]any, key string) (time.Time, error) {
	s, ok := args[key].(string)
	if !ok || s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339 (e.g. 2006-01-02T15:04:05Z), got %q", key, s)
	}
	return t, nil
}

func (s *Service) agentIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(agentIDContextKey).(string)
	return id
}

// toolSetAlert creates a news alert in the DB using category/market filters.
func (s *Service) toolSetAlert(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	agentID := s.agentIDFromCtx(ctx)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}
	if s.repo == nil {
		return nil, fmt.Errorf("news alerts require DB support (repo not configured)")
	}

	markets := parseStringArray(args, "markets")
	categories := parseStringArray(args, "categories")
	if len(markets) == 0 && len(categories) == 0 {
		return nil, fmt.Errorf("at least one of 'markets' or 'categories' is required")
	}

	titleContains, _ := args["title_contains"].(string)
	note, _ := args["note"].(string)
	onTrigger, _ := args["on_trigger"].(string)
	if onTrigger == "" {
		onTrigger = "wake_full"
	}
	if onTrigger != "wake_full" && onTrigger != "wake_triage" {
		return nil, fmt.Errorf("on_trigger must be wake_full or wake_triage")
	}
	maxTriggers := optInt(args, "max_triggers", 1)

	count, err := s.repo.CountActiveAlerts(ctx, agentID, "news")
	if err != nil {
		return nil, fmt.Errorf("set alert: count check: %w", err)
	}
	if count >= 10 {
		return nil, fmt.Errorf("set alert: limit reached (%d active news alerts, max 10)", count)
	}

	raw := fmt.Sprintf("%s:news:%s:%s:%s", agentID, strings.Join(markets, ","), strings.Join(categories, ","), titleContains)
	sum := sha256.Sum256([]byte(raw))
	alertID := "newsalert-" + hex.EncodeToString(sum[:])[:16]

	params := map[string]any{}
	if len(markets) > 0 {
		params["markets"] = markets
	}
	if len(categories) > 0 {
		params["categories"] = categories
	}
	if titleContains != "" {
		params["title_contains"] = titleContains
	}

	record := &db.AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "news",
		Status:      "active",
		OnTrigger:   onTrigger,
		MaxTriggers: maxTriggers,
		Params:      params,
		Note:        note,
	}
	if err := s.repo.UpsertAlert(ctx, record); err != nil {
		return nil, fmt.Errorf("set alert: %w", err)
	}

	return mcp.JSONResult(map[string]any{
		"alert_id":       alertID,
		"markets":        markets,
		"categories":     categories,
		"title_contains": titleContains,
		"on_trigger":     onTrigger,
		"status":         "active",
	})
}

// toolGetNewsTriggeredAlerts fetches and acks triggered news alerts for this agent.
func (s *Service) toolGetNewsTriggeredAlerts(ctx context.Context) (*mcp.ToolResult, error) {
	agentID := s.agentIDFromCtx(ctx)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required (set X-Agent-ID header)")
	}
	if s.repo == nil {
		return mcp.JSONResult(map[string]any{"alerts": []any{}})
	}

	triggered, err := s.repo.GetTriggeredAlerts(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get triggered alerts: %w", err)
	}

	var result []map[string]any
	var alertIDs []string
	for _, a := range triggered {
		if a.Service != "news" {
			continue
		}
		entry := map[string]any{
			"alert_id":  a.AlertID,
			"note":      a.Note,
			"on_trigger": a.OnTrigger,
		}
		if v, ok := a.Params["keyword"]; ok {
			entry["keyword"] = v
		}
		if v, ok := a.Params["markets"]; ok {
			entry["markets"] = v
		}
		if v, ok := a.Params["categories"]; ok {
			entry["categories"] = v
		}
		if v, ok := a.Params["title_contains"]; ok {
			entry["title_contains"] = v
		}
		if a.TriggeredAt != nil {
			entry["triggered_at"] = a.TriggeredAt.Format(time.RFC3339)
		}
		result = append(result, entry)
		alertIDs = append(alertIDs, a.AlertID)
	}

	if len(alertIDs) > 0 {
		if ackErr := s.repo.AckTriggeredAlerts(ctx, alertIDs); ackErr != nil {
			s.log.Warn("get_triggered_alerts: ack failed", zap.Error(ackErr))
		}
	}

	return mcp.JSONResult(map[string]any{"alerts": result})
}

// runKeywordAlertPoller polls news for matching articles and marks alerts triggered.
func (s *Service) runKeywordAlertPoller() {
	ticker := time.NewTicker(s.alertPollInterval)
	defer ticker.Stop()
	ctx := s.svcCtx.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkKeywordAlerts(ctx)
		}
	}
}

func (s *Service) checkKeywordAlerts(ctx context.Context) {
	alerts, err := s.repo.GetActiveAlerts(ctx, "", "news")
	if err != nil {
		s.log.Warn("News alert poll failed", zap.Error(err))
		return
	}
	if len(alerts) == 0 {
		return
	}

	since := time.Now().Add(-s.alertPollInterval * 2)

	for _, a := range alerts {
		markets := parseAnyStringSlice(a.Params["markets"])
		categories := parseAnyStringSlice(a.Params["categories"])
		titleContains, _ := a.Params["title_contains"].(string)

		// Legacy alerts with "keyword" param: treat keyword as title_contains.
		if kw, _ := a.Params["keyword"].(string); kw != "" && titleContains == "" {
			titleContains = kw
		}

		if len(markets) == 0 && len(categories) == 0 {
			continue
		}

		articles, searchErr := s.source.Search(ctx, newsdata.SearchParams{
			Markets:    markets,
			Categories: categories,
			DateFrom:   since,
			Limit:      10,
		})
		if searchErr != nil {
			s.log.Warn("News alert search failed", zap.String("alert_id", a.AlertID), zap.Error(searchErr))
			continue
		}
		if len(articles) == 0 {
			continue
		}

		// Client-side title filter if specified.
		if titleContains != "" {
			needle := strings.ToLower(titleContains)
			filtered := articles[:0]
			for _, art := range articles {
				if strings.Contains(strings.ToLower(art.Title), needle) {
					filtered = append(filtered, art)
				}
			}
			if len(filtered) == 0 {
				continue
			}
			articles = filtered
		}

		triggered, markErr := s.repo.MarkAlertTriggered(ctx, a.AlertID, "")
		if markErr != nil {
			s.log.Warn("Mark news alert triggered failed", zap.String("alert_id", a.AlertID), zap.Error(markErr))
			continue
		}
		if triggered {
			s.log.Info("News alert triggered",
				zap.String("alert_id", a.AlertID),
				zap.String("agent_id", a.AgentID),
				zap.Strings("markets", markets),
				zap.Strings("categories", categories),
				zap.Int("articles", len(articles)),
			)
		}
	}
}

// parseAnyStringSlice converts a []any (from JSON params) to []string.
func parseAnyStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
