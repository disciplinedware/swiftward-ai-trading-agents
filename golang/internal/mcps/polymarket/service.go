package polymarket

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/platform"
)

const httpTimeout = 15 * time.Second

// Service implements the Polymarket MCP - read-only prediction market data.
type Service struct {
	svcCtx *platform.ServiceContext
	log    *zap.Logger
	gamma  *GammaClient
	clob   *CLOBClient
}

// NewService creates the Polymarket MCP service.
func NewService(svcCtx *platform.ServiceContext) *Service {
	return &Service{
		svcCtx: svcCtx,
		log:    svcCtx.Logger().Named("polymarket_mcp"),
		gamma:  NewGammaClient(""),
		clob:   NewCLOBClient(""),
	}
}

// NewTestService creates a Service with explicit dependencies for testing.
func NewTestService(log *zap.Logger, gamma *GammaClient, clob *CLOBClient) *Service {
	return &Service{
		log:   log,
		gamma: gamma,
		clob:  clob,
	}
}

func (s *Service) Initialize() error {
	mcpServer := mcp.NewServer("polymarket-mcp", "1.0.0", s.tools(), s.handleTool)
	s.svcCtx.Router().Post("/mcp/polymarket", mcpServer.ServeHTTP)
	s.log.Info("Polymarket MCP registered", zap.String("path", "/mcp/polymarket"))
	return nil
}

func (s *Service) Start() error {
	<-s.svcCtx.Context().Done()
	return nil
}

func (s *Service) Stop() error {
	return nil
}

func (s *Service) tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "polymarket/search_markets",
			Description: "Search Polymarket prediction markets grouped by event. Returns crowd-sourced probabilities, volume, and liquidity for upcoming events. Use to gauge market sentiment on crypto prices, geopolitics, politics, tech, and more. Results are grouped by event - each event contains related markets with odds.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Text filter on event title or market question (case-insensitive)",
					},
					"category": map[string]any{
						"type":        "string",
						"description": "Tag filter: Crypto, Geopolitics, Politics, Sports, AI, Finance, Tech, or omit for all categories",
					},
					"sort_by": map[string]any{
						"type":        "string",
						"enum":        []string{"volume", "newest"},
						"description": "Sort events by trading volume (default) or creation date",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of events to return, 1-20 (default: 10)",
					},
				},
			},
		},
		{
			Name:        "polymarket/get_market",
			Description: "Get full details for a specific Polymarket market: description, resolution criteria, current odds, order book depth, fees, and related markets in the same event. Use market_id from search_markets results.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"market_id": map[string]any{
						"type":        "string",
						"description": "Market ID from search_markets results",
					},
				},
				"required": []string{"market_id"},
			},
		},
	}
}

func (s *Service) handleTool(ctx context.Context, toolName string, args map[string]any) (*mcp.ToolResult, error) {
	switch toolName {
	case "polymarket/search_markets":
		return s.ToolSearchMarkets(ctx, args)
	case "polymarket/get_market":
		return s.ToolGetMarket(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// categoryTagIDs maps the user-facing category names to Gamma API tag IDs.
// The /markets endpoint ignores the tag param; /events supports tag_id.
var categoryTagIDs = map[string]string{
	"Crypto":      "21",
	"Geopolitics": "100265",
	"Politics":    "2",
	"Sports":      "1",
	"AI":          "439",
	"Finance":     "120",
	"Tech":        "1401",
}

// ToolSearchMarkets implements the polymarket/search_markets tool.
// Returns events with their markets grouped, not flat market list.
func (s *Service) ToolSearchMarkets(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	query, _ := args["query"].(string)
	category, _ := args["category"].(string)
	sortBy, _ := args["sort_by"].(string)

	limit := 10
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if limit > 20 {
		limit = 20
	}

	order, ascending := mapSortBy(sortBy)
	tagID := categoryTagIDs[category]

	// Fetch more events than needed to allow for client-side filtering.
	fetchLimit := limit * 3
	if fetchLimit < 50 {
		fetchLimit = 50
	}

	events, err := s.gamma.ListEvents(ctx, ListEventsParams{
		TagID:     tagID,
		Order:     order,
		Ascending: ascending,
		Limit:     fetchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	filtered := filterEvents(events, query, limit)

	text := formatEventResults(filtered)
	s.log.Info(fmt.Sprintf("Searching Polymarket: %q (%d results)", query, len(filtered)),
		zap.String("category", category),
		zap.String("query", query),
		zap.Int("events", len(filtered)),
	)
	return mcp.TextResult(text), nil
}

// filterEvents applies text query matching against event title and market questions.
// Also strips near-resolved markets within each event.
func filterEvents(events []GammaEvent, query string, limit int) []GammaEvent {
	queryLower := strings.ToLower(query)
	var result []GammaEvent
	for _, e := range events {
		// Filter markets within the event: remove near-resolved.
		var liveMarkets []GammaMarket
		for _, m := range e.Markets {
			if len(m.OutcomePrices) == 2 {
				p := parseFloat(m.OutcomePrices[0])
				if p > 0.99 || p < 0.01 {
					continue
				}
			}
			liveMarkets = append(liveMarkets, m)
		}
		if len(liveMarkets) == 0 {
			continue
		}

		// Text query: match against event title OR any market question.
		if query != "" {
			if strings.Contains(strings.ToLower(e.Title), queryLower) {
				// Event title matches - keep all live markets.
			} else {
				// Check individual markets.
				var matching []GammaMarket
				for _, m := range liveMarkets {
					if strings.Contains(strings.ToLower(m.Question), queryLower) {
						matching = append(matching, m)
					}
				}
				if len(matching) == 0 {
					continue
				}
				liveMarkets = matching
			}
		}

		e.Markets = liveMarkets
		result = append(result, e)
		if len(result) >= limit {
			break
		}
	}
	return result
}

// ToolGetMarket implements the polymarket/get_market tool.
func (s *Service) ToolGetMarket(ctx context.Context, args map[string]any) (*mcp.ToolResult, error) {
	marketID, _ := args["market_id"].(string)
	if marketID == "" {
		return nil, fmt.Errorf("market_id is required")
	}

	market, err := s.gamma.GetMarket(ctx, marketID)
	if err != nil {
		return nil, fmt.Errorf("get market: %w", err)
	}

	// Order book for YES token (first clobTokenId)
	var book *OrderBook
	if len(market.ClobTokenIDs) > 0 {
		b, err := s.clob.GetBook(ctx, market.ClobTokenIDs[0])
		if err != nil {
			s.log.Warn("failed to fetch order book", zap.String("market_id", marketID), zap.Error(err))
		} else {
			book = b
		}
	}

	// Parent event for related markets
	var event *GammaEvent
	if len(market.Events) > 0 {
		e, err := s.gamma.GetEvent(ctx, market.Events[0].ID)
		if err != nil {
			s.log.Warn("failed to fetch event", zap.String("event_id", market.Events[0].ID), zap.Error(err))
		} else {
			event = e
		}
	}

	text := formatMarketDetail(market, book, event)
	s.log.Info(fmt.Sprintf("Fetching Polymarket market %s", marketID), zap.String("market_id", marketID))
	return mcp.TextResult(text), nil
}

// mapSortBy converts the tool's sort_by param to Gamma API order + ascending flag.
func mapSortBy(sortBy string) (order string, ascending bool) {
	switch sortBy {
	case "newest":
		return "createdAt", false
	default: // volume
		return "volume24hr", false
	}
}
