package marketdata

import (
	"context"
	"fmt"
	"time"
)

// Interval represents a candle time interval.
type Interval string

const (
	Interval1m  Interval = "1m"
	Interval5m  Interval = "5m"
	Interval15m Interval = "15m"
	Interval1h  Interval = "1h"
	Interval4h  Interval = "4h"
	Interval1d  Interval = "1d"
)

// IntervalDuration returns the duration of the interval.
func (i Interval) Duration() time.Duration {
	switch i {
	case Interval1m:
		return time.Minute
	case Interval5m:
		return 5 * time.Minute
	case Interval15m:
		return 15 * time.Minute
	case Interval1h:
		return time.Hour
	case Interval4h:
		return 4 * time.Hour
	case Interval1d:
		return 24 * time.Hour
	default:
		return 0
	}
}

// ParseInterval validates and returns an Interval from a string.
func ParseInterval(s string) (Interval, error) {
	switch Interval(s) {
	case Interval1m, Interval5m, Interval15m, Interval1h, Interval4h, Interval1d:
		return Interval(s), nil
	default:
		return "", fmt.Errorf("invalid interval %q (valid: 1m, 5m, 15m, 1h, 4h, 1d)", s)
	}
}

// Ticker represents a current price snapshot for a market.
type Ticker struct {
	Market      string    `json:"market"`        // Canonical symbol (e.g., "ETH-USDC")
	Bid         string    `json:"bid"`           // Best bid price (decimal string)
	Ask         string    `json:"ask"`           // Best ask price (decimal string)
	Last        string    `json:"last"`          // Last trade price (decimal string)
	Volume24h   string    `json:"volume_24h"`    // 24h volume in quote currency (decimal string)
	Change24hPct string   `json:"change_24h_pct"` // 24h % change (decimal string)
	High24h     string    `json:"high_24h"`      // 24h high (decimal string)
	Low24h      string    `json:"low_24h"`       // 24h low (decimal string)
	Timestamp   time.Time `json:"timestamp"`
}

// Candle represents a single OHLCV bar.
type Candle struct {
	Timestamp time.Time `json:"t"`  // Candle open time
	Open      string    `json:"o"`  // Open price (decimal string)
	High      string    `json:"h"`  // High price (decimal string)
	Low       string    `json:"l"`  // Low price (decimal string)
	Close     string    `json:"c"`  // Close price (decimal string)
	Volume    string    `json:"v"`  // Volume in quote currency (decimal string)
}

// OrderbookLevel represents a single price level in the orderbook.
type OrderbookLevel struct {
	Price string `json:"price"` // decimal string
	Size  string `json:"size"`  // decimal string
}

// Orderbook represents the current orderbook state.
type Orderbook struct {
	Market    string           `json:"market"`
	Bids      []OrderbookLevel `json:"bids"`      // Sorted best (highest) first
	Asks      []OrderbookLevel `json:"asks"`      // Sorted best (lowest) first
	Timestamp time.Time        `json:"timestamp"`
}

// MarketInfo describes an available trading pair.
type MarketInfo struct {
	Pair        string `json:"pair"`           // Canonical symbol
	Base        string `json:"base"`           // Base currency (e.g., "ETH")
	Quote       string `json:"quote"`          // Quote currency (e.g., "USDC")
	LastPrice   string `json:"last_price"`     // Last trade price (decimal string)
	Volume24h   string `json:"volume_24h"`     // 24h volume (decimal string)
	Change24hPct string `json:"change_24h_pct"` // 24h % change (decimal string)
	Tradeable   bool   `json:"tradeable"`      // Available on the trading exchange
}

// FundingRate represents a single funding rate snapshot.
type FundingRate struct {
	Timestamp time.Time `json:"timestamp"`
	Rate      string    `json:"rate"` // decimal string (e.g., "0.000125")
}

// FundingData contains current and historical funding rates.
type FundingData struct {
	Market          string        `json:"market"`
	CurrentRate     string        `json:"current_rate"`      // decimal string
	AnnualizedPct   string        `json:"annualized_pct"`    // decimal string
	FundingIntervalH int          `json:"funding_interval_h"` // hours between settlements (8, 4, 1)
	NextFundingTime time.Time     `json:"next_funding_time"`
	History         []FundingRate `json:"history"`
}

// InferFundingIntervalH computes the funding interval in hours from history timestamps.
// Falls back to 8h if fewer than 2 data points.
// Uses minute-based rounding to handle timestamps that are slightly off (e.g. 7h59m).
func InferFundingIntervalH(history []FundingRate) int {
	if len(history) < 2 {
		return 8
	}
	// Find two consecutive timestamps (history may be newest-first or oldest-first).
	t0 := history[0].Timestamp
	t1 := history[1].Timestamp
	diff := t1.Sub(t0)
	if diff < 0 {
		diff = -diff
	}
	// Round to nearest hour via minutes to avoid truncation issues (e.g. 7h59m -> 8h).
	minutes := int(diff.Minutes())
	hours := (minutes + 30) / 60 // round to nearest hour
	if hours <= 0 {
		return 8
	}
	return hours
}

// OpenInterest represents the current open interest state.
type OpenInterest struct {
	Market         string `json:"market"`
	OpenInterest   string `json:"open_interest"`       // decimal string (notional value)
	OIChange1hPct  string `json:"oi_change_1h_pct"`    // decimal string
	OIChange4hPct  string `json:"oi_change_4h_pct"`    // decimal string
	OIChange24hPct string `json:"oi_change_24h_pct"`   // decimal string
	LongShortRatio string `json:"long_short_ratio"`    // decimal string
}

// DataSource provides market data from any provider.
// Implementations: SimulatedSource, BinanceSource, CompositeSource.
type DataSource interface {
	// GetTicker returns current price snapshots.
	// If symbols is empty, returns all available.
	GetTicker(ctx context.Context, symbols []string) ([]Ticker, error)

	// GetCandles returns OHLCV bars in chronological order (oldest first).
	// endTime zero = now. limit 0 = source default.
	GetCandles(ctx context.Context, symbol string, interval Interval, limit int, endTime time.Time) ([]Candle, error)

	// GetOrderbook returns bids/asks for a market.
	// depth 0 = source default (typically 20).
	GetOrderbook(ctx context.Context, symbol string, depth int) (*Orderbook, error)

	// GetMarkets returns available trading pairs with metadata.
	// quote filters by quote currency (e.g. "USD"); empty = all pairs.
	GetMarkets(ctx context.Context, quote string) ([]MarketInfo, error)

	// GetFundingRates returns current + historical funding rates for a perp market.
	// limit 0 = source default (typically 10).
	GetFundingRates(ctx context.Context, symbol string, limit int) (*FundingData, error)

	// GetOpenInterest returns OI with change deltas.
	GetOpenInterest(ctx context.Context, symbol string) (*OpenInterest, error)

	// Name returns the source identifier (for logging/debugging).
	Name() string
}
