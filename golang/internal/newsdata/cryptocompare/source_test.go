package cryptocompare

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
	now := time.Now()
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
				Data: []apiArticle{
					{
						Title:       "Bitcoin hits new high",
						PublishedOn: now.Unix(),
						URL:         "https://coindesk.com/1",
						Sentiment:   "POSITIVE",
						SourceData:  apiSourceData{Name: "CoinDesk"},
						CategoryData: []apiCategoryData{
							{Name: "BTC"}, {Name: "MARKET"},
						},
					},
					{
						Title:       "Ethereum upgrade announced",
						PublishedOn: now.Unix(),
						URL:         "https://theblock.co/2",
						Sentiment:   "NEUTRAL",
						SourceData:  apiSourceData{Name: "The Block"},
						CategoryData: []apiCategoryData{
							{Name: "ETH"},
						},
					},
				},
			},
			params:    newsdata.SearchParams{Query: "Bitcoin", Limit: 10},
			wantCount: 2,
		},
		{
			name:       "API error returns error",
			params:     newsdata.SearchParams{Query: "test"},
			statusCode: 500,
			wantErr:    true,
		},
		{
			name:      "empty response",
			response:  apiResponse{Data: []apiArticle{}},
			params:    newsdata.SearchParams{Markets: []string{"BTC"}, Limit: 10},
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
				w.WriteHeader(statusCode)
				if statusCode == http.StatusOK {
					_ = json.NewEncoder(w).Encode(tt.response)
				}
			}))
			defer srv.Close()

			src := NewSource(Config{BaseURL: srv.URL})
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

func TestSearchCombinesMarketsAndCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cats := r.URL.Query().Get("categories")
		// Markets (BTC) and Categories (REGULATION) should be combined.
		if cats != "BTC,REGULATION" {
			t.Errorf("categories param = %q, want %q", cats, "BTC,REGULATION")
		}
		_ = json.NewEncoder(w).Encode(apiResponse{Data: []apiArticle{}})
	}))
	defer srv.Close()

	src := NewSource(Config{BaseURL: srv.URL})
	_, _ = src.Search(context.Background(), newsdata.SearchParams{
		Markets:    []string{"BTC"},
		Categories: []string{"REGULATION"},
		Limit:      5,
	})
}

func TestSearchDoesNotMutateInputSlices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(apiResponse{Data: []apiArticle{}})
	}))
	defer srv.Close()

	src := NewSource(Config{BaseURL: srv.URL})
	// Create a markets slice with spare capacity.
	markets := make([]string, 1, 5)
	markets[0] = "ETH"
	categories := []string{"TRADING"}

	_, _ = src.Search(context.Background(), newsdata.SearchParams{
		Markets:    markets,
		Categories: categories,
		Limit:      5,
	})

	// Verify the original slice was not mutated.
	if len(markets) != 1 || markets[0] != "ETH" {
		t.Errorf("markets slice was mutated: %v", markets)
	}
}

func TestGetLatest(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		response  apiResponse
		params    newsdata.LatestParams
		wantCount int
	}{
		{
			name: "returns latest articles",
			response: apiResponse{
				Data: []apiArticle{
					{Title: "Breaking: BTC surge", PublishedOn: now.Unix(), SourceData: apiSourceData{Name: "CoinDesk"}},
					{Title: "ETH 2.0 update", PublishedOn: now.Unix(), SourceData: apiSourceData{Name: "The Block"}},
				},
			},
			params:    newsdata.LatestParams{Limit: 10},
			wantCount: 2,
		},
		{
			name:      "empty response",
			response:  apiResponse{Data: []apiArticle{}},
			params:    newsdata.LatestParams{Limit: 10},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			src := NewSource(Config{BaseURL: srv.URL})
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
	now := time.Now()
	tests := []struct {
		name          string
		response      apiResponse
		params        newsdata.SentimentParams
		wantSentiment string
		wantCount     int
	}{
		{
			name: "positive sentiment from API labels",
			response: apiResponse{
				Data: []apiArticle{
					{Title: "BTC bullish", PublishedOn: now.Unix(), Sentiment: "POSITIVE", Upvotes: 10, Downvotes: 1, CategoryData: []apiCategoryData{{Name: "BTC"}}},
					{Title: "BTC adoption", PublishedOn: now.Unix(), Sentiment: "POSITIVE", Upvotes: 8, Downvotes: 0, CategoryData: []apiCategoryData{{Name: "BTC"}}},
				},
			},
			params:        newsdata.SentimentParams{Query: "BTC", Period: "24h"},
			wantSentiment: "positive",
			wantCount:     2,
		},
		{
			name: "negative sentiment from API labels",
			response: apiResponse{
				Data: []apiArticle{
					{Title: "BTC crash", PublishedOn: now.Unix(), Sentiment: "NEGATIVE", Upvotes: 1, Downvotes: 20, CategoryData: []apiCategoryData{{Name: "BTC"}}},
				},
			},
			params:        newsdata.SentimentParams{Query: "BTC", Period: "24h"},
			wantSentiment: "negative",
			wantCount:     1,
		},
		{
			name:          "empty results",
			response:      apiResponse{Data: []apiArticle{}},
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

			src := NewSource(Config{BaseURL: srv.URL})
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

func TestToArticle(t *testing.T) {
	now := time.Now()
	subtitle := "A summary"
	tests := []struct {
		name          string
		article       apiArticle
		wantSentiment string
		wantMarkets   int
		wantSummary   string
	}{
		{
			name: "positive sentiment mapped",
			article: apiArticle{
				Title: "BTC up", PublishedOn: now.Unix(), Sentiment: "POSITIVE",
				SourceData:   apiSourceData{Name: "CoinDesk"},
				CategoryData: []apiCategoryData{{Name: "BTC"}, {Name: "MARKET"}},
			},
			wantSentiment: "positive",
			wantMarkets:   1, // only BTC is a crypto ticker, MARKET is not
		},
		{
			name: "subtitle becomes summary",
			article: apiArticle{
				Title: "ETH news", PublishedOn: now.Unix(), Sentiment: "NEUTRAL",
				Subtitle:   &subtitle,
				SourceData: apiSourceData{Name: "The Block"},
			},
			wantSentiment: "neutral",
			wantSummary:   "A summary",
		},
		{
			name: "empty sentiment defaults to neutral",
			article: apiArticle{
				Title: "SOL update", PublishedOn: now.Unix(), Sentiment: "",
				SourceData: apiSourceData{Name: "Decrypt"},
			},
			wantSentiment: "neutral",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := toArticle(tt.article)
			if a.Sentiment != tt.wantSentiment {
				t.Errorf("sentiment = %q, want %q", a.Sentiment, tt.wantSentiment)
			}
			if tt.wantMarkets > 0 && len(a.Markets) != tt.wantMarkets {
				t.Errorf("markets = %d, want %d", len(a.Markets), tt.wantMarkets)
			}
			if tt.wantSummary != "" && a.Summary != tt.wantSummary {
				t.Errorf("summary = %q, want %q", a.Summary, tt.wantSummary)
			}
		})
	}
}

func TestExtractMarkets(t *testing.T) {
	tests := []struct {
		name string
		cats []apiCategoryData
		want int
	}{
		{"crypto tickers only", []apiCategoryData{{Name: "BTC"}, {Name: "MARKET"}, {Name: "ETH"}}, 2},
		{"no tickers", []apiCategoryData{{Name: "MARKET"}, {Name: "TRADING"}}, 0},
		{"empty", []apiCategoryData{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMarkets(tt.cats)
			if len(got) != tt.want {
				t.Errorf("extractMarkets() got %d, want %d", len(got), tt.want)
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
		{"unknown", 24*time.Hour + time.Second},
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
