//go:build integration
// +build integration

// Integration tests for the Binance market data source.
// These tests hit the real Binance public API (no API key required).
// Run with: go test -tags=integration ./internal/marketdata/binance/integration/...
//
// All tests skip gracefully on network failure (geo-block, rate limit, etc.)
// so they never break CI in restricted environments.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
	"ai-trading-agents/internal/marketdata/binance"
)

func newSource(t *testing.T) *binance.Source {
	t.Helper()
	log, _ := zap.NewDevelopment()
	registry := marketdata.NewSymbolRegistry()
	return binance.NewSource(registry, binance.Config{}, log)
}

func TestGetTicker_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	tickers, err := src.GetTicker(ctx, []string{"ETH-USDC", "BTC-USDC"})
	if err != nil {
		t.Skipf("Binance spot API unavailable: %v", err)
	}
	if len(tickers) != 2 {
		t.Skipf("unexpected ticker count %d (API may be rate-limited)", len(tickers))
	}

	for _, tk := range tickers {
		assert.NotEmpty(t, tk.Market)
		assert.NotEmpty(t, tk.Last, "last price should be non-empty for %s", tk.Market)
		assert.NotEmpty(t, tk.Bid)
		assert.NotEmpty(t, tk.Ask)
	}

	t.Logf("ETH-USDC last price: %s", tickers[0].Last)
}

func TestGetCandles_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	candles, err := src.GetCandles(ctx, "ETH-USDC", marketdata.Interval1h, 10, time.Time{})
	if err != nil {
		t.Skipf("Binance spot API unavailable: %v", err)
	}

	assert.Len(t, candles, 10, "expected exactly 10 closed 1h candles")
	for _, c := range candles {
		assert.NotEmpty(t, c.Open)
		assert.NotEmpty(t, c.Close)
		assert.False(t, c.Timestamp.IsZero())
	}

	t.Logf("Latest 1h candle: open=%s close=%s at %s", candles[len(candles)-1].Open, candles[len(candles)-1].Close, candles[len(candles)-1].Timestamp)
}

func TestGetOrderbook_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ob, err := src.GetOrderbook(ctx, "ETH-USDC", 10)
	if err != nil {
		t.Skipf("Binance spot API unavailable: %v", err)
	}

	assert.Equal(t, "ETH-USDC", ob.Market)
	assert.NotEmpty(t, ob.Bids)
	assert.NotEmpty(t, ob.Asks)

	t.Logf("Best bid: %s, best ask: %s", ob.Bids[0].Price, ob.Asks[0].Price)
}

func TestGetMarkets_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	markets, err := src.GetMarkets(ctx, "")
	if err != nil {
		t.Skipf("Binance spot API unavailable: %v", err)
	}
	if len(markets) == 0 {
		t.Skip("Binance returned empty markets list")
	}

	found := map[string]bool{}
	for _, m := range markets {
		found[m.Pair] = true
	}
	assert.True(t, found["ETH-USDC"], "ETH-USDC should be in markets")
	assert.True(t, found["BTC-USDC"], "BTC-USDC should be in markets")

	t.Logf("Total markets: %d", len(markets))
}

// TestGetFundingRates_Integration tests the futures funding rate endpoint.
// Skips if the endpoint is geo-blocked (Binance restricts fapi in some regions).
func TestGetFundingRates_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fd, err := src.GetFundingRates(ctx, "ETH-USDC", 5)
	if err != nil {
		t.Skipf("Binance futures API unavailable (geo-block or endpoint change): %v", err)
	}

	assert.Equal(t, "ETH-USDC", fd.Market)
	assert.NotEmpty(t, fd.CurrentRate)
	assert.NotEmpty(t, fd.AnnualizedPct)
	assert.NotEmpty(t, fd.History)

	t.Logf("ETH-USDC funding rate: %s (annualized: %s%%)", fd.CurrentRate, fd.AnnualizedPct)
}

// TestGetOpenInterest_Integration tests the futures OI history endpoint.
// Skips if the endpoint is geo-blocked (Binance restricts fapi in some regions).
func TestGetOpenInterest_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	oi, err := src.GetOpenInterest(ctx, "ETH-USDC")
	if err != nil {
		t.Skipf("Binance futures API unavailable (geo-block or endpoint change): %v", err)
	}

	assert.Equal(t, "ETH-USDC", oi.Market)
	assert.NotEmpty(t, oi.OpenInterest)

	t.Logf("ETH-USDC OI: %s (1h: %s%%, 4h: %s%%, 24h: %s%%)",
		oi.OpenInterest, oi.OIChange1hPct, oi.OIChange4hPct, oi.OIChange24hPct)
}
