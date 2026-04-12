package newsdata

import (
	"context"
	"time"
)

// Article represents a news article from any source.
type Article struct {
	Title       string    `json:"title"`
	Source      string    `json:"source"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
	Summary     string    `json:"summary,omitempty"`
	Sentiment   string    `json:"sentiment,omitempty"` // "positive", "negative", "neutral"
	Markets     []string  `json:"markets,omitempty"`   // related markets, e.g. ["BTC", "ETH"]
	Kind        string    `json:"kind,omitempty"`      // "news", "media", "blog", etc.
}

// SentimentResult holds aggregated sentiment for a query over a time window.
type SentimentResult struct {
	Query        string   `json:"query"`
	Sentiment    string   `json:"sentiment"`     // "positive", "negative", "neutral"
	Score        float64  `json:"score"`          // -1.0 to 1.0
	ArticleCount int      `json:"article_count"`
	KeyThemes    []string `json:"key_themes,omitempty"`
	Period       string   `json:"period"`
}

// Event represents an upcoming market-moving event.
type Event struct {
	Title       string `json:"title"`
	Type        string `json:"type"`         // "fork", "upgrade", "regulation", "hack", "listing", "unlock", "macro"
	Date        string `json:"date"`         // ISO-8601
	ImpactLevel string `json:"impact_level"` // "high", "medium", "low"
	Details     string `json:"details,omitempty"`
	Market      string `json:"market,omitempty"` // related asset, e.g. "ETH"
}

// SearchParams holds parameters for news search.
type SearchParams struct {
	Query      string
	Markets    []string // filter by crypto ticker symbols (BTC, ETH, ...)
	Categories []string // filter by topic tags (REGULATION, TRADING, ...)
	DateFrom   time.Time
	DateTo     time.Time
	Limit      int
	Filter     string // "rising", "hot", "bullish", "bearish", "important"
	Kind       string // "news", "media", "all"
}

// LatestParams holds parameters for getting latest news.
type LatestParams struct {
	Kind    string // "news", "media", "all"
	Markets []string
	Filter  string // "rising", "hot", "bullish", "bearish", "important"
	Limit   int
}

// SentimentParams holds parameters for sentiment queries.
type SentimentParams struct {
	Query   string
	Markets []string
	Period  string // "1h", "4h", "24h", "7d"
}

// Source defines the interface for news data providers.
type Source interface {
	// Name returns the source identifier (e.g. "cryptopanic").
	Name() string

	// Search returns articles matching the search parameters.
	Search(ctx context.Context, params SearchParams) ([]Article, error)

	// GetLatest returns the most recent articles.
	GetLatest(ctx context.Context, params LatestParams) ([]Article, error)

	// GetSentiment returns aggregated sentiment for a query.
	GetSentiment(ctx context.Context, params SentimentParams) (*SentimentResult, error)
}
