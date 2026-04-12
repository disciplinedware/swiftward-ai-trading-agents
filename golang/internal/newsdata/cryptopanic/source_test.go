package cryptopanic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-trading-agents/internal/newsdata"
)

func TestSearch(t *testing.T) {
	tests := []struct {
		name       string
		response   apiResponse
		params     newsdata.SearchParams
		wantCount  int
		wantErr    bool
		statusCode int
	}{
		{
			name: "basic search returns articles",
			response: apiResponse{
				Results: []apiPost{
					{
						Title:       "Bitcoin hits new high",
						PublishedAt: time.Now().Format(time.RFC3339),
						Kind:        "news",
						Source:      apiSource{Title: "CoinDesk"},
						URL:         "https://coindesk.com/1",
						Currencies:  []apiCurrency{{Code: "BTC", Title: "Bitcoin"}},
						Votes:       apiVotes{Positive: 10, Liked: 5},
					},
					{
						Title:       "Ethereum upgrade announced",
						PublishedAt: time.Now().Format(time.RFC3339),
						Kind:        "news",
						Source:      apiSource{Title: "The Block"},
						URL:         "https://theblock.co/2",
						Currencies:  []apiCurrency{{Code: "ETH", Title: "Ethereum"}},
						Votes:       apiVotes{Positive: 8, Important: 3},
					},
				},
			},
			params:    newsdata.SearchParams{Query: "Bitcoin", Limit: 10},
			wantCount: 1, // only Bitcoin matches query filter
		},
		{
			name: "search with market filter",
			response: apiResponse{
				Results: []apiPost{
					{
						Title:       "ETH DeFi growth",
						PublishedAt: time.Now().Format(time.RFC3339),
						Kind:        "news",
						Source:      apiSource{Title: "CoinTelegraph"},
						URL:         "https://ct.com/1",
						Currencies:  []apiCurrency{{Code: "ETH"}},
						Votes:       apiVotes{Positive: 3},
					},
				},
			},
			params:    newsdata.SearchParams{Query: "ETH", Markets: []string{"ETH"}, Limit: 10},
			wantCount: 1,
		},
		{
			name:       "API error returns error",
			params:     newsdata.SearchParams{Query: "test"},
			statusCode: 500,
			wantErr:    true,
		},
		{
			name: "limit is respected",
			response: apiResponse{
				Results: []apiPost{
					{Title: "Article 1", PublishedAt: time.Now().Format(time.RFC3339), Source: apiSource{Title: "S1"}, Currencies: []apiCurrency{{Code: "BTC"}}},
					{Title: "Article 2", PublishedAt: time.Now().Format(time.RFC3339), Source: apiSource{Title: "S2"}, Currencies: []apiCurrency{{Code: "BTC"}}},
					{Title: "Article 3", PublishedAt: time.Now().Format(time.RFC3339), Source: apiSource{Title: "S3"}, Currencies: []apiCurrency{{Code: "BTC"}}},
				},
			},
			params:    newsdata.SearchParams{Query: "BTC", Limit: 2},
			wantCount: 2,
		},
		{
			name:      "no results for nonexistent query",
			response:  apiResponse{Results: []apiPost{}},
			params:    newsdata.SearchParams{Query: "nonexistent", Limit: 10},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusCode := tt.statusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify auth_token is sent
				if r.URL.Query().Get("auth_token") != "test-token" {
					t.Errorf("expected auth_token=test-token, got %q", r.URL.Query().Get("auth_token"))
				}
				w.WriteHeader(statusCode)
				if statusCode == http.StatusOK {
					_ = json.NewEncoder(w).Encode(tt.response)
				}
			}))
			defer srv.Close()

			src := NewSource(Config{
				AuthToken: "test-token",
				BaseURL:   srv.URL,
			})

			articles, err := src.Search(context.Background(), tt.params)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Search() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(articles) != tt.wantCount {
				t.Errorf("Search() got %d articles, want %d", len(articles), tt.wantCount)
			}
		})
	}
}

func TestGetLatest(t *testing.T) {
	tests := []struct {
		name      string
		response  apiResponse
		params    newsdata.LatestParams
		wantCount int
	}{
		{
			name: "returns latest articles",
			response: apiResponse{
				Results: []apiPost{
					{Title: "Breaking: BTC surge", PublishedAt: time.Now().Format(time.RFC3339), Kind: "news", Source: apiSource{Title: "CoinDesk"}},
					{Title: "ETH 2.0 update", PublishedAt: time.Now().Format(time.RFC3339), Kind: "news", Source: apiSource{Title: "The Block"}},
				},
			},
			params:    newsdata.LatestParams{Limit: 10},
			wantCount: 2,
		},
		{
			name:      "empty response",
			response:  apiResponse{Results: []apiPost{}},
			params:    newsdata.LatestParams{Limit: 10},
			wantCount: 0,
		},
		{
			name: "kind filter maps to API kind",
			response: apiResponse{
				Results: []apiPost{
					{Title: "News article", Kind: "news", PublishedAt: time.Now().Format(time.RFC3339), Source: apiSource{Title: "S1"}},
				},
			},
			params:    newsdata.LatestParams{Kind: "news", Limit: 10},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			src := NewSource(Config{AuthToken: "test-token", BaseURL: srv.URL})
			articles, err := src.GetLatest(context.Background(), tt.params)
			if err != nil {
				t.Fatalf("GetLatest() error = %v", err)
			}
			if len(articles) != tt.wantCount {
				t.Errorf("GetLatest() got %d articles, want %d", len(articles), tt.wantCount)
			}
		})
	}
}

func TestGetSentiment(t *testing.T) {
	tests := []struct {
		name          string
		response      apiResponse
		params        newsdata.SentimentParams
		wantSentiment string
		wantCount     int
	}{
		{
			name: "positive sentiment from votes",
			response: apiResponse{
				Results: []apiPost{
					{
						Title:       "BTC bullish signal",
						PublishedAt: time.Now().Format(time.RFC3339),
						Kind:        "news",
						Source:      apiSource{Title: "S1"},
						Currencies:  []apiCurrency{{Code: "BTC"}},
						Votes:       apiVotes{Positive: 20, Liked: 10, Important: 5, Negative: 2},
					},
					{
						Title:       "BTC adoption grows",
						PublishedAt: time.Now().Format(time.RFC3339),
						Kind:        "news",
						Source:      apiSource{Title: "S2"},
						Currencies:  []apiCurrency{{Code: "BTC"}},
						Votes:       apiVotes{Positive: 15, Liked: 8, Negative: 1},
					},
				},
			},
			params:        newsdata.SentimentParams{Query: "BTC", Period: "24h"},
			wantSentiment: "positive",
			wantCount:     2,
		},
		{
			name: "negative sentiment from votes",
			response: apiResponse{
				Results: []apiPost{
					{
						Title:       "BTC crash incoming",
						PublishedAt: time.Now().Format(time.RFC3339),
						Source:      apiSource{Title: "S1"},
						Currencies:  []apiCurrency{{Code: "BTC"}},
						Votes:       apiVotes{Negative: 20, Disliked: 10, Toxic: 5, Positive: 2},
					},
				},
			},
			params:        newsdata.SentimentParams{Query: "BTC", Period: "24h"},
			wantSentiment: "negative",
			wantCount:     1,
		},
		{
			name: "neutral sentiment when no votes",
			response: apiResponse{
				Results: []apiPost{
					{
						Title:       "BTC market update",
						PublishedAt: time.Now().Format(time.RFC3339),
						Source:      apiSource{Title: "S1"},
						Currencies:  []apiCurrency{{Code: "BTC"}},
						Votes:       apiVotes{},
					},
				},
			},
			params:        newsdata.SentimentParams{Query: "BTC", Period: "24h"},
			wantSentiment: "neutral",
			wantCount:     1,
		},
		{
			name:          "empty results",
			response:      apiResponse{Results: []apiPost{}},
			params:        newsdata.SentimentParams{Query: "UNKNOWN", Period: "1h"},
			wantSentiment: "neutral",
			wantCount:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			src := NewSource(Config{AuthToken: "test-token", BaseURL: srv.URL})
			result, err := src.GetSentiment(context.Background(), tt.params)
			if err != nil {
				t.Fatalf("GetSentiment() error = %v", err)
			}
			if result.Sentiment != tt.wantSentiment {
				t.Errorf("GetSentiment() sentiment = %q, want %q", result.Sentiment, tt.wantSentiment)
			}
			if result.ArticleCount != tt.wantCount {
				t.Errorf("GetSentiment() article_count = %d, want %d", result.ArticleCount, tt.wantCount)
			}
		})
	}
}

func TestClassifyPostSentiment(t *testing.T) {
	tests := []struct {
		name string
		votes apiVotes
		want  string
	}{
		{"strongly positive", apiVotes{Positive: 20, Liked: 10, Negative: 1}, "positive"},
		{"strongly negative", apiVotes{Negative: 20, Disliked: 10, Toxic: 5, Positive: 1}, "negative"},
		{"neutral with no votes", apiVotes{}, "neutral"},
		{"balanced votes", apiVotes{Positive: 10, Negative: 10}, "neutral"},
		{"slightly positive still neutral", apiVotes{Positive: 6, Negative: 5}, "neutral"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyPostSentiment(tt.votes)
			if got != tt.want {
				t.Errorf("classifyPostSentiment() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterByQuery(t *testing.T) {
	posts := []apiPost{
		{Title: "Bitcoin hits $100k", Currencies: []apiCurrency{{Code: "BTC", Title: "Bitcoin"}}},
		{Title: "Ethereum merge complete", Currencies: []apiCurrency{{Code: "ETH", Title: "Ethereum"}}},
		{Title: "Crypto market overview", Currencies: []apiCurrency{{Code: "BTC"}, {Code: "ETH"}}},
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"title match", "Bitcoin", 1},
		{"currency code match", "ETH", 2},  // title "Ethereum" + currency code "ETH" on overview
		{"case insensitive", "bitcoin", 1},
		{"no match", "Solana", 0},
		{"partial match", "market", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByQuery(posts, tt.query)
			if len(got) != tt.want {
				t.Errorf("filterByQuery(%q) got %d, want %d", tt.query, len(got), tt.want)
			}
		})
	}
}

func TestPeriodToCutoff(t *testing.T) {
	tests := []struct {
		period string
		maxAge time.Duration
	}{
		{"1h", 1*time.Hour + time.Second},
		{"4h", 4*time.Hour + time.Second},
		{"24h", 24*time.Hour + time.Second},
		{"7d", 7*24*time.Hour + time.Second},
		{"unknown", 24*time.Hour + time.Second}, // defaults to 24h
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			cutoff := periodToCutoff(tt.period)
			age := time.Since(cutoff)
			if age > tt.maxAge {
				t.Errorf("periodToCutoff(%q): age %v exceeds max %v", tt.period, age, tt.maxAge)
			}
		})
	}
}

func TestTopThemes(t *testing.T) {
	tests := []struct {
		name   string
		themes map[string]int
		n      int
		want   int
	}{
		{"empty", map[string]int{}, 5, 0},
		{"fewer than n", map[string]int{"BTC": 5, "ETH": 3}, 5, 2},
		{"exact n", map[string]int{"BTC": 5, "ETH": 3, "SOL": 1}, 3, 3},
		{"more than n", map[string]int{"BTC": 5, "ETH": 3, "SOL": 1, "DOGE": 2}, 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := topThemes(tt.themes, tt.n)
			if len(got) != tt.want {
				t.Errorf("topThemes() got %d themes, want %d", len(got), tt.want)
			}
			// Check ordering (first should be highest count)
			if len(got) >= 2 && tt.themes[got[0]] < tt.themes[got[1]] {
				t.Errorf("topThemes() not sorted: %s(%d) < %s(%d)", got[0], tt.themes[got[0]], got[1], tt.themes[got[1]])
			}
		})
	}
}

func TestFilterByDate(t *testing.T) {
	now := time.Now()
	posts := []apiPost{
		{Title: "Old article", PublishedAt: now.Add(-48 * time.Hour).Format(time.RFC3339)},
		{Title: "Recent article", PublishedAt: now.Add(-2 * time.Hour).Format(time.RFC3339)},
		{Title: "Very recent", PublishedAt: now.Add(-10 * time.Minute).Format(time.RFC3339)},
	}

	tests := []struct {
		name string
		from time.Time
		to   time.Time
		want int
	}{
		{"no filter", time.Time{}, time.Time{}, 3},
		{"from 24h ago", now.Add(-24 * time.Hour), time.Time{}, 2},
		{"from 1h ago", now.Add(-1 * time.Hour), time.Time{}, 1},
		{"to 1h ago", time.Time{}, now.Add(-1 * time.Hour), 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByDate(posts, tt.from, tt.to)
			if len(got) != tt.want {
				t.Errorf("filterByDate() got %d, want %d", len(got), tt.want)
			}
		})
	}
}
