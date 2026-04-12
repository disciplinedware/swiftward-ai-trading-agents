package prism

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Circuit breaker states.
const (
	stateClosed   = "closed"
	stateOpen     = "open"
	stateHalfOpen = "half_open"
)

// Config holds PRISM client configuration.
type Config struct {
	BaseURL          string
	APIKey           string
	Timeout          time.Duration
	FailureThreshold int
	Cooldown         time.Duration
}

// Client wraps the PRISM API with circuit breaker.
// Rate limiting is server-side (PRISM returns 429) - the circuit breaker
// handles it like any other failure, so we don't track limits client-side.
type Client struct {
	cfg    Config
	http   *http.Client
	log    *zap.Logger
	now    func() time.Time // injectable clock for testing

	mu                sync.Mutex
	state             string
	consecutiveErrors int
	openedAt          time.Time
}

// NewClient creates a PRISM API client with circuit breaker.
func NewClient(cfg Config, log *zap.Logger) *Client {
	return &Client{
		cfg:   cfg,
		http:  &http.Client{Timeout: cfg.Timeout},
		log:   log.Named("prism"),
		now:   time.Now,
		state: stateClosed,
	}
}

// FearGreedResponse is the /market/fear-greed response.
type FearGreedResponse struct {
	Value    int      `json:"value"`
	Label    string   `json:"label"`
	Warnings []string `json:"warnings"`
}

// TechnicalsResponse is the /technicals/{symbol} response.
type TechnicalsResponse struct {
	Symbol         string   `json:"symbol"`
	Timeframe      string   `json:"timeframe"`
	RSI            *float64 `json:"rsi"`
	RSISignal      string   `json:"rsi_signal"`
	MACD           *float64 `json:"macd"`
	MACDSignal     *float64 `json:"macd_signal"`
	MACDHistogram  *float64 `json:"macd_histogram"`
	MACDTrend      string   `json:"macd_trend"`
	SMA20          *float64 `json:"sma_20"`
	SMA50          *float64 `json:"sma_50"`
	SMA200         *float64 `json:"sma_200"`
	EMA12          *float64 `json:"ema_12"`
	EMA26          *float64 `json:"ema_26"`
	EMA50          *float64 `json:"ema_50"`
	BBUpper        *float64 `json:"bb_upper"`
	BBMiddle       *float64 `json:"bb_middle"`
	BBLower        *float64 `json:"bb_lower"`
	BBWidth        *float64 `json:"bb_width"`
	StochK         *float64 `json:"stoch_k"`
	StochD         *float64 `json:"stoch_d"`
	StochSignal    string   `json:"stoch_signal"`
	ATR            *float64 `json:"atr"`
	ATRPercent     *float64 `json:"atr_percent"`
	ADX            *float64 `json:"adx"`
	PlusDI         *float64 `json:"plus_di"`
	MinusDI        *float64 `json:"minus_di"`
	ADXTrend       string   `json:"adx_trend"`
	WilliamsR      *float64 `json:"williams_r"`
	CCI            *float64 `json:"cci"`
	CurrentPrice   *float64 `json:"current_price"`
	PriceChange24h *float64 `json:"price_change_24h"`
	OverallSignal  string   `json:"overall_signal"`
	BullishSignals int      `json:"bullish_signals"`
	BearishSignals int      `json:"bearish_signals"`
	Timestamp      string   `json:"timestamp"`
}

// SignalEntry is one asset in the signals summary.
type SignalEntry struct {
	Symbol        string         `json:"symbol"`
	OverallSignal string         `json:"overall_signal"`
	Direction     string         `json:"direction"`
	Strength      string         `json:"strength"`
	BullishScore  int            `json:"bullish_score"`
	BearishScore  int            `json:"bearish_score"`
	NetScore      int            `json:"net_score"`
	CurrentPrice  *float64       `json:"current_price"`
	Indicators    map[string]any `json:"indicators"`
	ActiveSignals []ActiveSignal `json:"active_signals"`
	SignalCount   int            `json:"signal_count"`
	Timestamp     string         `json:"timestamp"`
}

// ActiveSignal describes a triggered signal.
type ActiveSignal struct {
	Type   string   `json:"type"`
	Signal string   `json:"signal"`
	Value  *float64 `json:"value"`
}

// SignalsSummaryResponse is the /signals/summary response.
type SignalsSummaryResponse struct {
	Data    []SignalEntry `json:"data"`
	Summary struct {
		Total         int `json:"total"`
		StrongBullish int `json:"strong_bullish"`
		Bullish       int `json:"bullish"`
		Neutral       int `json:"neutral"`
		Bearish       int `json:"bearish"`
		StrongBearish int `json:"strong_bearish"`
	} `json:"summary"`
}

// UnavailableError is returned when the circuit breaker is open or rate limit hit.
type UnavailableError struct {
	Reason          string
	RetryAfterSecs  int
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("prism unavailable: %s (retry after %ds)", e.Reason, e.RetryAfterSecs)
}

// GetFearGreed calls GET /market/fear-greed.
func (c *Client) GetFearGreed(ctx context.Context) (*FearGreedResponse, error) {
	var resp FearGreedResponse
	if err := c.doGet(ctx, "/market/fear-greed", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetTechnicals calls GET /technicals/{symbol}.
// symbol should be base asset only (e.g. "BTC", not "BTC-USDC").
func (c *Client) GetTechnicals(ctx context.Context, symbol string) (*TechnicalsResponse, error) {
	var resp TechnicalsResponse
	if err := c.doGet(ctx, "/technicals/"+symbol, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetSignalsSummary calls GET /signals/summary?symbols=SYM1,SYM2.
// symbols should be base assets (e.g. ["BTC", "ETH"]).
func (c *Client) GetSignalsSummary(ctx context.Context, symbols []string) (*SignalsSummaryResponse, error) {
	path := "/signals/summary"
	if len(symbols) > 0 {
		path += "?symbols=" + strings.Join(symbols, ",")
	}
	// Parse the nested response structure.
	var raw struct {
		Data     []SignalEntry `json:"data"`
		Metadata struct {
			Summary struct {
				Total         int `json:"total"`
				StrongBullish int `json:"strong_bullish"`
				Bullish       int `json:"bullish"`
				Neutral       int `json:"neutral"`
				Bearish       int `json:"bearish"`
				StrongBearish int `json:"strong_bearish"`
			} `json:"summary"`
		} `json:"metadata"`
	}
	if err := c.doGet(ctx, path, &raw); err != nil {
		return nil, err
	}
	return &SignalsSummaryResponse{
		Data:    raw.Data,
		Summary: raw.Metadata.Summary,
	}, nil
}

// doGet executes a GET request with circuit breaker and rate limit checks.
func (c *Client) doGet(ctx context.Context, path string, out any) error {
	if err := c.beforeCall(); err != nil {
		return err
	}

	err := c.httpGet(ctx, path, out)
	c.afterCall(err)
	return err
}

// beforeCall checks circuit breaker state.
// Returns UnavailableError if the client should not make a call.
func (c *Client) beforeCall() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()

	switch c.state {
	case stateClosed:
		return nil

	case stateOpen:
		// Check if cooldown has elapsed.
		if now.Sub(c.openedAt) >= c.cfg.Cooldown {
			c.state = stateHalfOpen
			c.log.Info("PRISM circuit breaker entering half-open state, probing")
			return nil
		}
		remaining := c.cfg.Cooldown - now.Sub(c.openedAt)
		return &UnavailableError{
			Reason:         "circuit breaker open",
			RetryAfterSecs: int(remaining.Seconds()) + 1,
		}

	case stateHalfOpen:
		return nil

	default:
		return fmt.Errorf("prism: unknown circuit breaker state %q", c.state)
	}
}

// afterCall updates circuit breaker state based on call result.
func (c *Client) afterCall(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err == nil {
		if c.state == stateHalfOpen {
			c.log.Info("PRISM circuit breaker closed, service recovered")
		}
		c.state = stateClosed
		c.consecutiveErrors = 0
		return
	}

	c.consecutiveErrors++
	c.log.Warn("PRISM API error",
		zap.Error(err),
		zap.Int("consecutive_errors", c.consecutiveErrors))

	if c.consecutiveErrors >= c.cfg.FailureThreshold && c.state != stateOpen {
		c.state = stateOpen
		c.openedAt = c.now()
		c.log.Warn("PRISM circuit breaker opened",
			zap.Int("consecutive_errors", c.consecutiveErrors),
			zap.Duration("cooldown", c.cfg.Cooldown))
	}
}

// httpGet performs the actual HTTP GET and JSON decode.
func (c *Client) httpGet(ctx context.Context, path string, out any) error {
	url := c.cfg.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("prism: build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("prism: request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return fmt.Errorf("prism: read response %s: %w", path, err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("prism: rate limited (429) on %s", path)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prism: %s returned %d: %s", path, resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("prism: decode %s: %w", path, err)
	}
	return nil
}

// State returns the current circuit breaker state (for observability).
func (c *Client) State() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// CanonicalToBase extracts the base asset from a canonical symbol.
// "ETH-USDC" -> "ETH", "BTC" -> "BTC".
func CanonicalToBase(symbol string) string {
	if idx := strings.Index(symbol, "-"); idx > 0 {
		return symbol[:idx]
	}
	return symbol
}
