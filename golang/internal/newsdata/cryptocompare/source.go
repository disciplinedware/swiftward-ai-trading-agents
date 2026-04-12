package cryptocompare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-trading-agents/internal/newsdata"
)

const (
	defaultBaseURL = "https://data-api.coindesk.com/news/v1"
	defaultTimeout = 10 * time.Second
)

// Config holds CryptoCompare/CoinDesk API configuration.
type Config struct {
	BaseURL string // override for testing
	Timeout time.Duration
}

// apiResponse is the top-level API response.
type apiResponse struct {
	Data []apiArticle `json:"Data"`
	Err  any          `json:"Err"`
}

// apiArticle represents a single article from the API.
type apiArticle struct {
	ID           int64             `json:"ID"`
	Title        string            `json:"TITLE"`
	Subtitle     *string           `json:"SUBTITLE"`
	Body         string            `json:"BODY"`
	URL          string            `json:"URL"`
	PublishedOn  int64             `json:"PUBLISHED_ON"`
	Keywords     string            `json:"KEYWORDS"`
	Sentiment    string            `json:"SENTIMENT"` // POSITIVE, NEGATIVE, NEUTRAL
	Upvotes      int               `json:"UPVOTES"`
	Downvotes    int               `json:"DOWNVOTES"`
	Lang         string            `json:"LANG"`
	SourceData   apiSourceData     `json:"SOURCE_DATA"`
	CategoryData []apiCategoryData `json:"CATEGORY_DATA"`
}

type apiSourceData struct {
	Name string `json:"NAME"`
}

type apiCategoryData struct {
	Name string `json:"NAME"`
}

// Source implements newsdata.Source using the CryptoCompare/CoinDesk API.
type Source struct {
	baseURL    string
	httpClient *http.Client
}

// NewSource creates a CryptoCompare news source.
func NewSource(cfg Config) *Source {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	return &Source{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (s *Source) Name() string { return "cryptocompare" }

func (s *Source) Search(ctx context.Context, params newsdata.SearchParams) ([]newsdata.Article, error) {
	q := url.Values{}
	q.Set("lang", "EN")
	q.Set("status", "ACTIVE")
	q.Set("sort_order", "DESC")

	// CoinDesk /article/list ignores q= param. Only categories work.
	// Combine ticker symbols (Markets) and topic tags (Categories) into one param.
	allCats := make([]string, 0, len(params.Markets)+len(params.Categories))
	allCats = append(allCats, params.Markets...)
	allCats = append(allCats, params.Categories...)
	if len(allCats) > 0 {
		q.Set("categories", strings.Join(allCats, ","))
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	q.Set("limit", strconv.Itoa(limit))

	if !params.DateFrom.IsZero() {
		q.Set("lts_after", strconv.FormatInt(params.DateFrom.Unix(), 10))
	}
	if !params.DateTo.IsZero() {
		q.Set("lts_before", strconv.FormatInt(params.DateTo.Unix(), 10))
	}

	articles, err := s.fetchArticles(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("search news: %w", err)
	}

	return articles, nil
}

func (s *Source) GetLatest(ctx context.Context, params newsdata.LatestParams) ([]newsdata.Article, error) {
	q := url.Values{}
	q.Set("lang", "EN")
	q.Set("status", "ACTIVE")
	q.Set("sort_order", "DESC")

	if len(params.Markets) > 0 {
		q.Set("categories", strings.Join(params.Markets, ","))
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	q.Set("limit", strconv.Itoa(limit))

	articles, err := s.fetchArticles(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get latest news: %w", err)
	}

	return articles, nil
}

func (s *Source) GetSentiment(ctx context.Context, params newsdata.SentimentParams) (*newsdata.SentimentResult, error) {
	q := url.Values{}
	q.Set("lang", "EN")
	q.Set("status", "ACTIVE")
	q.Set("sort_order", "DESC")
	q.Set("limit", "50") // fetch more for sentiment aggregation

	if len(params.Markets) > 0 {
		q.Set("categories", strings.Join(params.Markets, ","))
	}
	// Apply time window
	if params.Period != "" {
		cutoff := periodToCutoff(params.Period)
		if !cutoff.IsZero() {
			q.Set("lts_after", strconv.FormatInt(cutoff.Unix(), 10))
		}
	}

	raw, err := s.fetchRawArticles(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get sentiment: %w", err)
	}

	return computeSentiment(params.Query, params.Period, raw), nil
}

// fetchArticles calls the API and returns converted articles.
func (s *Source) fetchArticles(ctx context.Context, params url.Values) ([]newsdata.Article, error) {
	raw, err := s.fetchRawArticles(ctx, params)
	if err != nil {
		return nil, err
	}
	return toArticles(raw), nil
}

// fetchRawArticles calls the CoinDesk API and returns raw articles.
func (s *Source) fetchRawArticles(ctx context.Context, params url.Values) ([]apiArticle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/article/list", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.URL.RawQuery = params.Encode()

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return apiResp.Data, nil
}

// toArticles converts API articles to our Article type.
func toArticles(raw []apiArticle) []newsdata.Article {
	articles := make([]newsdata.Article, 0, len(raw))
	for _, a := range raw {
		articles = append(articles, toArticle(a))
	}
	return articles
}

func toArticle(a apiArticle) newsdata.Article {
	published := time.Unix(a.PublishedOn, 0).UTC()

	// Extract market symbols from categories
	markets := extractMarkets(a.CategoryData)

	sentiment := strings.ToLower(a.Sentiment)
	if sentiment == "" {
		sentiment = "neutral"
	}

	summary := ""
	if a.Subtitle != nil && *a.Subtitle != "" {
		summary = *a.Subtitle
	}

	return newsdata.Article{
		Title:       a.Title,
		Source:      a.SourceData.Name,
		URL:         a.URL,
		PublishedAt: published,
		Summary:     summary,
		Sentiment:   sentiment,
		Markets:     markets,
		Kind:        "news",
	}
}

// extractMarkets picks crypto ticker symbols from categories (BTC, ETH, SOL, etc.).
func extractMarkets(cats []apiCategoryData) []string {
	// Known crypto ticker categories - others like "MARKET", "EXCHANGE", "TRADING" are topic tags
	cryptoTickers := map[string]bool{
		"BTC": true, "ETH": true, "SOL": true, "XRP": true, "ADA": true,
		"DOGE": true, "DOT": true, "AVAX": true, "LINK": true, "MATIC": true,
		"UNI": true, "AAVE": true, "LTC": true, "BCH": true, "ATOM": true,
		"ARB": true, "OP": true, "APT": true, "SUI": true, "NEAR": true,
		"FIL": true, "ICP": true, "ALGO": true, "FTM": true, "INJ": true,
		"TIA": true, "SEI": true, "RENDER": true, "PEPE": true, "WIF": true,
		"BONK": true, "SHIB": true, "BNB": true, "TRX": true, "TON": true,
		"USDT": true, "USDC": true, "DAI": true, "RUNE": true, "CRV": true,
		"MKR": true, "SNX": true, "COMP": true, "SUSHI": true, "YFI": true,
	}

	var markets []string
	for _, c := range cats {
		if cryptoTickers[c.Name] {
			markets = append(markets, c.Name)
		}
	}
	return markets
}

// computeSentiment aggregates sentiment across articles.
func computeSentiment(query, period string, articles []apiArticle) *newsdata.SentimentResult {
	if len(articles) == 0 {
		return &newsdata.SentimentResult{
			Query:        query,
			Sentiment:    "neutral",
			Score:        0,
			ArticleCount: 0,
			Period:       period,
		}
	}

	positive, negative := 0, 0
	themes := map[string]int{}

	for _, a := range articles {
		switch strings.ToUpper(a.Sentiment) {
		case "POSITIVE":
			positive++
		case "NEGATIVE":
			negative++
		}
		// Also factor in votes
		positive += a.Upvotes
		negative += a.Downvotes

		for _, c := range a.CategoryData {
			themes[c.Name]++
		}
	}

	total := positive + negative
	score := 0.0
	if total > 0 {
		score = float64(positive-negative) / float64(total)
	}

	sentiment := "neutral"
	switch {
	case score > 0.2:
		sentiment = "positive"
	case score < -0.2:
		sentiment = "negative"
	}

	return &newsdata.SentimentResult{
		Query:        query,
		Sentiment:    sentiment,
		Score:        score,
		ArticleCount: len(articles),
		KeyThemes:    topThemes(themes, 5),
		Period:       period,
	}
}

// topThemes returns the top N themes by frequency.
func topThemes(themes map[string]int, n int) []string {
	if len(themes) == 0 {
		return nil
	}

	type kv struct {
		Key   string
		Count int
	}
	var sorted []kv
	for k, v := range themes {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})

	result := make([]string, 0, n)
	for i := 0; i < n && i < len(sorted); i++ {
		result = append(result, sorted[i].Key)
	}
	return result
}

// periodToCutoff converts a period string to a cutoff time.
func periodToCutoff(period string) time.Time {
	now := time.Now()
	switch period {
	case "1h":
		return now.Add(-1 * time.Hour)
	case "4h":
		return now.Add(-4 * time.Hour)
	case "24h":
		return now.Add(-24 * time.Hour)
	case "7d":
		return now.Add(-7 * 24 * time.Hour)
	default:
		return now.Add(-24 * time.Hour)
	}
}
