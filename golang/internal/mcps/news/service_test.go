package news

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/mcp"
	"ai-trading-agents/internal/newsdata"
	"ai-trading-agents/internal/platform"

	"github.com/go-chi/chi/v5"
)

// mockSource implements newsdata.Source for testing.
type mockSource struct {
	articles   []newsdata.Article
	sentiment  *newsdata.SentimentResult
	searchErr  error
	latestErr  error
	sentErr    error
}

func (m *mockSource) Name() string { return "mock" }

func (m *mockSource) Search(_ context.Context, _ newsdata.SearchParams) ([]newsdata.Article, error) {
	return m.articles, m.searchErr
}

func (m *mockSource) GetLatest(_ context.Context, _ newsdata.LatestParams) ([]newsdata.Article, error) {
	return m.articles, m.latestErr
}

func (m *mockSource) GetSentiment(_ context.Context, _ newsdata.SentimentParams) (*newsdata.SentimentResult, error) {
	return m.sentiment, m.sentErr
}

func testServiceContext(t *testing.T) *platform.ServiceContext {
	t.Helper()
	log, _ := zap.NewDevelopment()
	router := chi.NewRouter()
	ctx := context.Background()
	return platform.NewServiceContext(ctx, log, nil, router, []string{"news_mcp"})
}

func newTestService(t *testing.T, src newsdata.Source) *Service {
	t.Helper()
	svcCtx := testServiceContext(t)
	return NewService(svcCtx, src, nil)
}

func parseJSON(t *testing.T, result *mcp.ToolResult) map[string]any {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].Text), &m); err != nil {
		t.Fatalf("failed to parse JSON: %v\ntext: %s", err, result.Content[0].Text)
	}
	return m
}

func TestToolSearch(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		args      map[string]any
		articles  []newsdata.Article
		searchErr error
		wantErr   bool
		wantCount int
	}{
		{
			name: "basic search by market",
			args: map[string]any{"markets": []any{"BTC"}},
			articles: []newsdata.Article{
				{Title: "Bitcoin hits $100k", Source: "CoinDesk", PublishedAt: now, Sentiment: "positive"},
			},
			wantCount: 1,
		},
		{
			name:      "empty args returns latest",
			args:      map[string]any{},
			wantCount: 0,
		},
		{
			name:      "source error",
			args:      map[string]any{"markets": []any{"BTC"}},
			searchErr: fmt.Errorf("API down"),
			wantErr:   true,
		},
		{
			name: "with all optional params",
			args: map[string]any{
				"markets":    []any{"ETH"},
				"categories": []any{"REGULATION"},
				"filter":     "hot",
				"kind":       "news",
				"date_from":  now.Add(-24 * time.Hour).Format(time.RFC3339),
				"date_to":    now.Format(time.RFC3339),
				"limit":      float64(5),
			},
			articles: []newsdata.Article{
				{Title: "ETH DeFi update", Source: "The Block", Sentiment: "neutral"},
			},
			wantCount: 1,
		},
		{
			name:    "invalid date_from",
			args:    map[string]any{"markets": []any{"BTC"}, "date_from": "not-a-date"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &mockSource{articles: tt.articles, searchErr: tt.searchErr}
			svc := newTestService(t, src)

			result, err := svc.handleTool(context.Background(), "news/search", tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("toolSearch() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			m := parseJSON(t, result)
			count := int(m["count"].(float64))
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
			if m["source"] != "mock" {
				t.Errorf("source = %v, want mock", m["source"])
			}
		})
	}
}

func TestToolGetLatest(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		articles  []newsdata.Article
		latestErr error
		wantErr   bool
		wantCount int
	}{
		{
			name: "basic get latest",
			args: map[string]any{},
			articles: []newsdata.Article{
				{Title: "Breaking news 1"},
				{Title: "Breaking news 2"},
			},
			wantCount: 2,
		},
		{
			name: "with kind and filter",
			args: map[string]any{
				"kind":    "news",
				"filter":  "hot",
				"markets": []any{"BTC", "ETH"},
				"limit":   float64(5),
			},
			articles:  []newsdata.Article{{Title: "Crypto hot news"}},
			wantCount: 1,
		},
		{
			name:      "source error",
			args:      map[string]any{},
			latestErr: fmt.Errorf("rate limited"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &mockSource{articles: tt.articles, latestErr: tt.latestErr}
			svc := newTestService(t, src)

			result, err := svc.handleTool(context.Background(), "news/get_latest", tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("toolGetLatest() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			m := parseJSON(t, result)
			count := int(m["count"].(float64))
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestToolGetSentiment(t *testing.T) {
	tests := []struct {
		name          string
		args          map[string]any
		sentiment     *newsdata.SentimentResult
		sentErr       error
		wantErr       bool
		wantSentiment string
	}{
		{
			name: "positive sentiment",
			args: map[string]any{"query": "BTC"},
			sentiment: &newsdata.SentimentResult{
				Query:        "BTC",
				Sentiment:    "positive",
				Score:        0.65,
				ArticleCount: 47,
				KeyThemes:    []string{"adoption", "institutional"},
				Period:       "24h",
			},
			wantSentiment: "positive",
		},
		{
			name:    "missing query",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "source error",
			args:    map[string]any{"query": "ETH"},
			sentErr: fmt.Errorf("timeout"),
			wantErr: true,
		},
		{
			name: "with period and markets",
			args: map[string]any{
				"query":   "ETH",
				"period":  "1h",
				"markets": []any{"ETH"},
			},
			sentiment: &newsdata.SentimentResult{
				Query:        "ETH",
				Sentiment:    "neutral",
				Score:        0.0,
				ArticleCount: 5,
				Period:       "1h",
			},
			wantSentiment: "neutral",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &mockSource{sentiment: tt.sentiment, sentErr: tt.sentErr}
			svc := newTestService(t, src)

			result, err := svc.handleTool(context.Background(), "news/get_sentiment", tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("toolGetSentiment() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			m := parseJSON(t, result)
			if m["sentiment"] != tt.wantSentiment {
				t.Errorf("sentiment = %v, want %v", m["sentiment"], tt.wantSentiment)
			}
			if m["source"] != "mock" {
				t.Errorf("source = %v, want mock", m["source"])
			}
		})
	}
}

func TestToolGetEvents(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		args      map[string]any
		articles  []newsdata.Article
		searchErr error
		wantErr   bool
		checkFn   func(t *testing.T, m map[string]any)
	}{
		{
			name: "classifies hack events",
			args: map[string]any{},
			articles: []newsdata.Article{
				{Title: "DeFi Protocol Hacked for $50M", Source: "CoinDesk", PublishedAt: now, Markets: []string{"ETH"}},
			},
			checkFn: func(t *testing.T, m map[string]any) {
				events := m["events"].([]any)
				if len(events) != 1 {
					t.Fatalf("expected 1 event, got %d", len(events))
				}
				evt := events[0].(map[string]any)
				if evt["type"] != "hack" {
					t.Errorf("type = %v, want hack", evt["type"])
				}
				if evt["impact_level"] != "high" {
					t.Errorf("impact_level = %v, want high", evt["impact_level"])
				}
			},
		},
		{
			name: "classifies regulation events",
			args: map[string]any{"type": "regulation"},
			articles: []newsdata.Article{
				{Title: "SEC Approves Bitcoin ETF", Source: "Reuters", PublishedAt: now, Markets: []string{"BTC"}},
				{Title: "Bitcoin hits $100k", Source: "CoinDesk", PublishedAt: now, Markets: []string{"BTC"}},
			},
			checkFn: func(t *testing.T, m map[string]any) {
				events := m["events"].([]any)
				if len(events) != 1 {
					t.Fatalf("expected 1 regulation event, got %d", len(events))
				}
				evt := events[0].(map[string]any)
				if evt["type"] != "regulation" {
					t.Errorf("type = %v, want regulation", evt["type"])
				}
			},
		},
		{
			name: "classifies upgrade events",
			args: map[string]any{},
			articles: []newsdata.Article{
				{Title: "Ethereum Pectra Upgrade Set for March", Source: "The Block", PublishedAt: now, Markets: []string{"ETH"}},
			},
			checkFn: func(t *testing.T, m map[string]any) {
				events := m["events"].([]any)
				if len(events) != 1 {
					t.Fatalf("expected 1 event, got %d", len(events))
				}
				evt := events[0].(map[string]any)
				if evt["type"] != "upgrade" {
					t.Errorf("type = %v, want upgrade", evt["type"])
				}
			},
		},
		{
			name: "classifies macro events",
			args: map[string]any{},
			articles: []newsdata.Article{
				{Title: "Fed Raises Interest Rate by 25bps", Source: "Bloomberg", PublishedAt: now},
			},
			checkFn: func(t *testing.T, m map[string]any) {
				events := m["events"].([]any)
				if len(events) != 1 {
					t.Fatalf("expected 1 event, got %d", len(events))
				}
				evt := events[0].(map[string]any)
				if evt["type"] != "macro" {
					t.Errorf("type = %v, want macro", evt["type"])
				}
			},
		},
		{
			name:      "source error",
			args:      map[string]any{},
			searchErr: fmt.Errorf("API error"),
			wantErr:   true,
		},
		{
			name: "days_ahead capped at 30",
			args: map[string]any{"days": float64(100)},
			articles: []newsdata.Article{},
			checkFn: func(t *testing.T, m map[string]any) {
				count := int(m["count"].(float64))
				if count != 0 {
					t.Errorf("expected 0 events, got %d", count)
				}
			},
		},
		{
			name: "days_ahead filters old articles",
			args: map[string]any{"days": float64(3)},
			articles: []newsdata.Article{
				{Title: "Recent Hack at DeFi Protocol", Source: "CoinDesk", PublishedAt: now.Add(-1 * 24 * time.Hour), Markets: []string{"ETH"}},
				{Title: "Old Exploit from Last Month", Source: "CoinDesk", PublishedAt: now.Add(-10 * 24 * time.Hour), Markets: []string{"ETH"}},
			},
			checkFn: func(t *testing.T, m map[string]any) {
				events := m["events"].([]any)
				if len(events) != 1 {
					t.Fatalf("expected 1 event (recent only), got %d", len(events))
				}
				evt := events[0].(map[string]any)
				if evt["type"] != "hack" {
					t.Errorf("type = %v, want hack", evt["type"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &mockSource{articles: tt.articles, searchErr: tt.searchErr}
			svc := newTestService(t, src)

			result, err := svc.handleTool(context.Background(), "news/get_events", tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("toolGetEvents() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			m := parseJSON(t, result)
			if tt.checkFn != nil {
				tt.checkFn(t, m)
			}
		})
	}
}

func TestHandleToolUnknown(t *testing.T) {
	svc := newTestService(t, &mockSource{})
	_, err := svc.handleTool(context.Background(), "news/unknown_tool", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestClassifyArticleAsEvent(t *testing.T) {
	tests := []struct {
		name       string
		title      string
		wantType   string
		wantImpact string
	}{
		{"hack", "DeFi Protocol Exploit Drains $100M", "hack", "high"},
		{"upgrade", "Ethereum Pectra Upgrade Goes Live", "upgrade", "high"},
		{"fork", "Bitcoin Cash Fork Scheduled", "fork", "high"},
		{"regulation", "SEC Files Lawsuit Against Binance", "regulation", "high"},
		{"listing", "New Altcoin Listing on Coinbase", "listing", "medium"},
		{"unlock", "Solana Token Unlock of 10M SOL", "unlock", "medium"},
		{"macro", "Fed FOMC Meeting Signals Rate Cut", "macro", "high"},
		{"unclassifiable returns nil", "Bitcoin Trading Volume Increases", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			article := newsdata.Article{Title: tt.title, Source: "Test"}
			evt := classifyArticleAsEvent(article)
			if tt.wantType == "" {
				if evt != nil {
					t.Fatalf("expected nil, got event type=%q", evt.Type)
				}
				return
			}
			if evt == nil {
				t.Fatal("expected event, got nil")
			}
			if evt.Type != tt.wantType {
				t.Errorf("type = %q, want %q", evt.Type, tt.wantType)
			}
			if evt.ImpactLevel != tt.wantImpact {
				t.Errorf("impact_level = %q, want %q", evt.ImpactLevel, tt.wantImpact)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		s    string
		subs []string
		want bool
	}{
		{"bitcoin hack discovered", []string{"hack", "exploit"}, true},
		{"normal trading update", []string{"hack", "exploit"}, false},
		{"", []string{"hack"}, false},
		{"hack", []string{}, false},
	}

	for _, tt := range tests {
		got := containsAny(tt.s, tt.subs...)
		if got != tt.want {
			t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.subs, got, tt.want)
		}
	}
}

func TestParseStringArray(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		key  string
		want int
	}{
		{"valid array", map[string]any{"markets": []any{"BTC", "ETH"}}, "markets", 2},
		{"empty array", map[string]any{"markets": []any{}}, "markets", 0},
		{"missing key", map[string]any{}, "markets", 0},
		{"wrong type", map[string]any{"markets": "BTC"}, "markets", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStringArray(tt.args, tt.key)
			if len(got) != tt.want {
				t.Errorf("parseStringArray() len = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestOptInt(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		key        string
		defaultVal int
		want       int
	}{
		{"present", map[string]any{"limit": float64(20)}, "limit", 10, 20},
		{"missing", map[string]any{}, "limit", 10, 10},
		{"zero", map[string]any{"limit": float64(0)}, "limit", 10, 10},
		{"negative", map[string]any{"limit": float64(-5)}, "limit", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optInt(tt.args, tt.key, tt.defaultVal)
			if got != tt.want {
				t.Errorf("optInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestToolDefinitions(t *testing.T) {
	svc := newTestService(t, &mockSource{})
	tools := svc.tools()

	expectedTools := map[string]bool{
		"news/search":              false,
		"news/get_latest":          false,
		"news/get_sentiment":       false,
		"news/get_events":          false,
		"news/set_alert":            false,
		"news/get_triggered_alerts": false,
	}

	if len(tools) != len(expectedTools) {
		t.Fatalf("expected %d tools, got %d", len(expectedTools), len(tools))
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; !ok {
			t.Errorf("unexpected tool: %s", tool.Name)
		}
		expectedTools[tool.Name] = true

		if tool.Description == "" {
			t.Errorf("tool %s has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s has nil input schema", tool.Name)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}
