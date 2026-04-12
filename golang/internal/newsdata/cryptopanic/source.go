package cryptopanic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"ai-trading-agents/internal/newsdata"
)

const (
	defaultBaseURL = "https://cryptopanic.com/api/free/v1"
	defaultTimeout = 10 * time.Second
)

// Config holds CryptoPanic API configuration.
type Config struct {
	AuthToken string // required for API access
	BaseURL   string // override for testing
	Timeout   time.Duration
}

// apiResponse is the top-level CryptoPanic API response.
type apiResponse struct {
	Count    int       `json:"count"`
	Next     string    `json:"next"`
	Previous string    `json:"previous"`
	Results  []apiPost `json:"results"`
}

// apiPost represents a single CryptoPanic post.
type apiPost struct {
	ID          int64          `json:"id"`
	Title       string         `json:"title"`
	PublishedAt string         `json:"published_at"`
	CreatedAt   string         `json:"created_at"`
	Kind        string         `json:"kind"` // "news", "media", "blog"
	Domain      string         `json:"domain"`
	URL         string         `json:"url"`
	Slug        string         `json:"slug"`
	Currencies  []apiCurrency  `json:"currencies"`
	Votes       apiVotes       `json:"votes"`
	Source      apiSource      `json:"source"`
}

type apiCurrency struct {
	Code  string `json:"code"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type apiVotes struct {
	Negative  int `json:"negative"`
	Positive  int `json:"positive"`
	Important int `json:"important"`
	Liked     int `json:"liked"`
	Disliked  int `json:"disliked"`
	Lol       int `json:"lol"`
	Toxic     int `json:"toxic"`
	Saved     int `json:"saved"`
	Comments  int `json:"comments"`
}

type apiSource struct {
	Title  string `json:"title"`
	Region string `json:"region"`
	Domain string `json:"domain"`
}

// Source implements newsdata.Source using the CryptoPanic API.
type Source struct {
	authToken  string
	baseURL    string
	httpClient *http.Client
}

// NewSource creates a CryptoPanic news source.
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
		authToken: cfg.AuthToken,
		baseURL:   strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (s *Source) Name() string { return "cryptopanic" }

func (s *Source) Search(ctx context.Context, params newsdata.SearchParams) ([]newsdata.Article, error) {
	q := url.Values{}
	if params.Filter != "" {
		q.Set("filter", params.Filter)
	}
	if params.Kind != "" && params.Kind != "all" {
		q.Set("kind", params.Kind)
	}
	if len(params.Markets) > 0 {
		q.Set("currencies", strings.Join(params.Markets, ","))
	}
	q.Set("public", "true")

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	posts, err := s.fetchPosts(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("search news: %w", err)
	}

	// Client-side filtering by query (CryptoPanic free tier doesn't support search keyword)
	if params.Query != "" {
		posts = filterByQuery(posts, params.Query)
	}

	// Client-side date filtering
	if !params.DateFrom.IsZero() || !params.DateTo.IsZero() {
		posts = filterByDate(posts, params.DateFrom, params.DateTo)
	}

	if len(posts) > limit {
		posts = posts[:limit]
	}

	return postsToArticles(posts), nil
}

func (s *Source) GetLatest(ctx context.Context, params newsdata.LatestParams) ([]newsdata.Article, error) {
	q := url.Values{}
	q.Set("public", "true")

	if params.Filter != "" {
		q.Set("filter", params.Filter)
	}

	if len(params.Markets) > 0 {
		q.Set("currencies", strings.Join(params.Markets, ","))
	}

	if params.Kind != "" && params.Kind != "all" {
		q.Set("kind", params.Kind)
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	posts, err := s.fetchPosts(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get latest news: %w", err)
	}

	if len(posts) > limit {
		posts = posts[:limit]
	}

	return postsToArticles(posts), nil
}

func (s *Source) GetSentiment(ctx context.Context, params newsdata.SentimentParams) (*newsdata.SentimentResult, error) {
	q := url.Values{}
	q.Set("public", "true")

	if len(params.Markets) > 0 {
		q.Set("currencies", strings.Join(params.Markets, ","))
	}

	posts, err := s.fetchPosts(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get sentiment: %w", err)
	}

	// Filter by query if provided
	if params.Query != "" {
		posts = filterByQuery(posts, params.Query)
	}

	// Filter by time period
	if params.Period != "" {
		cutoff := periodToCutoff(params.Period)
		if !cutoff.IsZero() {
			posts = filterByDate(posts, cutoff, time.Time{})
		}
	}

	return computeSentiment(params.Query, params.Period, posts), nil
}

// fetchPosts calls the CryptoPanic API and returns raw posts.
func (s *Source) fetchPosts(ctx context.Context, params url.Values) ([]apiPost, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/posts/", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Add auth token to the request URL query (not materialized as a string
	// in application code, so it won't leak via url.Error on transport failures).
	params.Set("auth_token", s.authToken)
	req.URL.RawQuery = params.Encode()

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", redactTokenFromError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("cryptopanic API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return apiResp.Results, nil
}

// postsToArticles converts CryptoPanic posts to our Article type.
func postsToArticles(posts []apiPost) []newsdata.Article {
	articles := make([]newsdata.Article, 0, len(posts))
	for _, p := range posts {
		articles = append(articles, postToArticle(p))
	}
	return articles
}

func postToArticle(p apiPost) newsdata.Article {
	published, _ := time.Parse(time.RFC3339, p.PublishedAt)

	markets := make([]string, 0, len(p.Currencies))
	for _, c := range p.Currencies {
		markets = append(markets, c.Code)
	}

	return newsdata.Article{
		Title:       p.Title,
		Source:      p.Source.Title,
		URL:         p.URL,
		PublishedAt: published,
		Sentiment:   classifyPostSentiment(p.Votes),
		Markets:     markets,
		Kind:        p.Kind,
	}
}

// classifyPostSentiment derives sentiment from CryptoPanic vote data.
func classifyPostSentiment(v apiVotes) string {
	bullish := v.Positive + v.Liked + v.Important + v.Saved
	bearish := v.Negative + v.Disliked + v.Toxic
	total := bullish + bearish

	if total == 0 {
		return "neutral"
	}

	ratio := float64(bullish-bearish) / float64(total)
	switch {
	case ratio > 0.2:
		return "positive"
	case ratio < -0.2:
		return "negative"
	default:
		return "neutral"
	}
}

// computeSentiment aggregates sentiment across posts.
func computeSentiment(query, period string, posts []apiPost) *newsdata.SentimentResult {
	if len(posts) == 0 {
		return &newsdata.SentimentResult{
			Query:        query,
			Sentiment:    "neutral",
			Score:        0,
			ArticleCount: 0,
			Period:       period,
		}
	}

	totalBullish := 0
	totalBearish := 0
	themes := map[string]int{}

	for _, p := range posts {
		bullish := p.Votes.Positive + p.Votes.Liked + p.Votes.Important + p.Votes.Saved
		bearish := p.Votes.Negative + p.Votes.Disliked + p.Votes.Toxic
		totalBullish += bullish
		totalBearish += bearish

		// Extract themes from currency mentions
		for _, c := range p.Currencies {
			themes[c.Code]++
		}
	}

	total := totalBullish + totalBearish
	score := 0.0
	if total > 0 {
		score = float64(totalBullish-totalBearish) / float64(total)
	}

	sentiment := "neutral"
	switch {
	case score > 0.2:
		sentiment = "positive"
	case score < -0.2:
		sentiment = "negative"
	}

	// Top themes by frequency
	keyThemes := topThemes(themes, 5)

	return &newsdata.SentimentResult{
		Query:        query,
		Sentiment:    sentiment,
		Score:        score,
		ArticleCount: len(posts),
		KeyThemes:    keyThemes,
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

// filterByQuery does client-side title matching (case-insensitive).
func filterByQuery(posts []apiPost, query string) []apiPost {
	q := strings.ToLower(query)
	var filtered []apiPost
	for _, p := range posts {
		if strings.Contains(strings.ToLower(p.Title), q) {
			filtered = append(filtered, p)
			continue
		}
		// Also match on currency codes
		for _, c := range p.Currencies {
			if strings.EqualFold(c.Code, query) || strings.EqualFold(c.Title, query) {
				filtered = append(filtered, p)
				break
			}
		}
	}
	return filtered
}

// filterByDate filters posts to those within [from, to].
func filterByDate(posts []apiPost, from, to time.Time) []apiPost {
	var filtered []apiPost
	for _, p := range posts {
		published, err := time.Parse(time.RFC3339, p.PublishedAt)
		if err != nil {
			continue
		}
		if !from.IsZero() && published.Before(from) {
			continue
		}
		if !to.IsZero() && published.After(to) {
			continue
		}
		filtered = append(filtered, p)
	}
	return filtered
}

// redactTokenFromError strips auth_token values from url.Error messages.
func redactTokenFromError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if u, parseErr := url.Parse(urlErr.URL); parseErr == nil {
			q := u.Query()
			if q.Has("auth_token") {
				q.Set("auth_token", "REDACTED")
				u.RawQuery = q.Encode()
				urlErr.URL = u.String()
			}
		}
		return urlErr
	}
	return err
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
		return now.Add(-24 * time.Hour) // default 24h
	}
}
