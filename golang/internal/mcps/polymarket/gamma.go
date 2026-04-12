package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const defaultGammaBaseURL = "https://gamma-api.polymarket.com"

// GammaMarket represents a single tradable market from the Gamma API.
type GammaMarket struct {
	ID               string          `json:"id"`
	Question         string          `json:"question"`
	Description      string          `json:"description"`
	ResolutionSource string          `json:"resolutionSource"`
	EndDate          string          `json:"endDate"`
	Outcomes         []string        `json:"outcomes"`
	OutcomePrices    []string        `json:"outcomePrices"`
	BestBid          float64         `json:"bestBid"`
	BestAsk          float64         `json:"bestAsk"`
	Spread           float64         `json:"spread"`
	LastTradePrice   float64         `json:"lastTradePrice"`
	Volume           string          `json:"volume"`
	Volume24hr       float64         `json:"volume24hr"`
	Volume1wk        float64         `json:"volume1wk"`
	LiquidityNum     float64         `json:"liquidityNum"`
	ClobTokenIDs     []string        `json:"clobTokenIds"`
	Active           bool            `json:"active"`
	Closed           bool            `json:"closed"`
	NegRisk          bool            `json:"negRisk"`
	FeeSchedule      *FeeSchedule    `json:"feeSchedule"`
	FeeType          string          `json:"feeType"`
	Events           []GammaEventRef `json:"events"`
}

// UnmarshalJSON handles Polymarket's inconsistent encoding where outcomes,
// outcomePrices, and clobTokenIds are returned as JSON-encoded strings
// (e.g. `"[\"Yes\",\"No\"]"`) rather than native JSON arrays.
func (m *GammaMarket) UnmarshalJSON(b []byte) error {
	type plain GammaMarket
	var raw struct {
		plain
		Outcomes      json.RawMessage `json:"outcomes"`
		OutcomePrices json.RawMessage `json:"outcomePrices"`
		ClobTokenIDs  json.RawMessage `json:"clobTokenIds"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*m = GammaMarket(raw.plain)
	if err := unmarshalStringOrArray(raw.Outcomes, &m.Outcomes); err != nil {
		return fmt.Errorf("outcomes: %w", err)
	}
	if err := unmarshalStringOrArray(raw.OutcomePrices, &m.OutcomePrices); err != nil {
		return fmt.Errorf("outcomePrices: %w", err)
	}
	if err := unmarshalStringOrArray(raw.ClobTokenIDs, &m.ClobTokenIDs); err != nil {
		return fmt.Errorf("clobTokenIds: %w", err)
	}
	return nil
}

// unmarshalStringOrArray decodes a field that the Gamma API may send as either
// a JSON array or a JSON-encoded string containing an array.
func unmarshalStringOrArray(data json.RawMessage, dst *[]string) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '[' {
		return json.Unmarshal(data, dst)
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return json.Unmarshal([]byte(s), dst)
}

// GammaEventRef is the trimmed event reference embedded in a market.
type GammaEventRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

// FeeSchedule holds Polymarket fee parameters.
type FeeSchedule struct {
	Rate       float64 `json:"rate"`
	TakerOnly  bool    `json:"takerOnly"`
	RebateRate float64 `json:"rebateRate"`
}

// GammaEvent is the full event object returned by /events/{id}.
type GammaEvent struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Markets []GammaMarket `json:"markets"`
}

// ListMarketsParams controls the /markets query.
type ListMarketsParams struct {
	Tag       string
	Order     string // volume24hr, createdAt, endDate, liquidity
	Ascending bool
	Limit     int
}

// GammaClient fetches market data from the Polymarket Gamma API (no auth required).
type GammaClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewGammaClient creates a Gamma API client.
func NewGammaClient(baseURL string) *GammaClient {
	if baseURL == "" {
		baseURL = defaultGammaBaseURL
	}
	return &GammaClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// ListMarkets calls GET /markets with the given params.
func (c *GammaClient) ListMarkets(ctx context.Context, p ListMarketsParams) ([]GammaMarket, error) {
	q := url.Values{}
	q.Set("active", "true")
	q.Set("closed", "false")
	if p.Tag != "" {
		q.Set("tag", p.Tag)
	}
	order := p.Order
	if order == "" {
		order = "volume24hr"
	}
	q.Set("order", order)
	q.Set("ascending", strconv.FormatBool(p.Ascending))
	limit := p.Limit
	if limit <= 0 {
		limit = 40
	}
	q.Set("limit", strconv.Itoa(limit))

	var markets []GammaMarket
	if err := c.get(ctx, "/markets?"+q.Encode(), &markets); err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}
	return markets, nil
}

// GetMarket calls GET /markets/{id}.
func (c *GammaClient) GetMarket(ctx context.Context, id string) (*GammaMarket, error) {
	var m GammaMarket
	if err := c.get(ctx, "/markets/"+url.PathEscape(id), &m); err != nil {
		return nil, fmt.Errorf("get market %s: %w", id, err)
	}
	return &m, nil
}

// GetEvent calls GET /events/{id}.
func (c *GammaClient) GetEvent(ctx context.Context, id string) (*GammaEvent, error) {
	var e GammaEvent
	if err := c.get(ctx, "/events/"+url.PathEscape(id), &e); err != nil {
		return nil, fmt.Errorf("get event %s: %w", id, err)
	}
	return &e, nil
}

// ListEventsParams controls the /events query.
type ListEventsParams struct {
	TagID     string
	Order     string
	Ascending bool
	Limit     int
}

// ListEvents calls GET /events with the given params. Unlike /markets,
// the /events endpoint supports tag_id filtering.
func (c *GammaClient) ListEvents(ctx context.Context, p ListEventsParams) ([]GammaEvent, error) {
	q := url.Values{}
	q.Set("active", "true")
	q.Set("closed", "false")
	if p.TagID != "" {
		q.Set("tag_id", p.TagID)
	}
	order := p.Order
	if order == "" {
		order = "volume24hr"
	}
	q.Set("order", order)
	q.Set("ascending", strconv.FormatBool(p.Ascending))
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	q.Set("limit", strconv.Itoa(limit))

	var events []GammaEvent
	if err := c.get(ctx, "/events?"+q.Encode(), &events); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return events, nil
}

func (c *GammaClient) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gamma API returned %d for %s", resp.StatusCode, path)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
