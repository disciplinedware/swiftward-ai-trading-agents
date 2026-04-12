package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const defaultCLOBBaseURL = "https://clob.polymarket.com"

// OrderBookLevel is a single price level in the order book.
type OrderBookLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// OrderBook is the CLOB order book for one token.
type OrderBook struct {
	Bids []OrderBookLevel `json:"bids"`
	Asks []OrderBookLevel `json:"asks"`
}

// CLOBClient fetches order book data from the Polymarket CLOB API (no auth for reads).
type CLOBClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewCLOBClient creates a CLOB API client.
func NewCLOBClient(baseURL string) *CLOBClient {
	if baseURL == "" {
		baseURL = defaultCLOBBaseURL
	}
	return &CLOBClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// GetBook calls GET /book?token_id={tokenID} and returns the order book.
func (c *CLOBClient) GetBook(ctx context.Context, tokenID string) (*OrderBook, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/book", nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("token_id", tokenID)
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get book for token %s: %w", tokenID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CLOB API returned %d for /book", resp.StatusCode)
	}
	var book OrderBook
	if err := json.NewDecoder(resp.Body).Decode(&book); err != nil {
		return nil, fmt.Errorf("decode order book: %w", err)
	}
	return &book, nil
}
