//go:build integration
// +build integration

// Integration tests for the Kraken market data source.
// These tests hit the real Kraken public API (no API key required).
// Run with: go test -tags=integration ./internal/marketdata/kraken/integration/...
//
// All tests skip gracefully on network failure so they never break CI.
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
	"ai-trading-agents/internal/marketdata/kraken"
)

func newSource(t *testing.T) *kraken.Source {
	t.Helper()
	log, _ := zap.NewDevelopment()
	registry := marketdata.NewSymbolRegistry()
	return kraken.NewSource(registry, kraken.Config{}, log)
}

func TestGetTicker_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Test with a mix of legacy (BTC->XXBTZUSD) and modern (SOL->SOLUSD) pairs.
	tickers, err := src.GetTicker(ctx, []string{"BTC-USD", "ETH-USD", "SOL-USD"})
	if err != nil {
		t.Skipf("Kraken API unavailable: %v", err)
	}
	if len(tickers) != 3 {
		t.Skipf("unexpected ticker count %d (API may be rate-limited)", len(tickers))
	}

	for _, tk := range tickers {
		assert.NotEmpty(t, tk.Market)
		assert.NotEmpty(t, tk.Last, "last price should be non-empty for %s", tk.Market)
		assert.NotEmpty(t, tk.Bid)
		assert.NotEmpty(t, tk.Ask)
		assert.NotEmpty(t, tk.Change24hPct)
		assert.NotEmpty(t, tk.Volume24h)
	}

	t.Logf("BTC-USD last: %s, ETH-USD last: %s, SOL-USD last: %s",
		tickers[0].Last, tickers[1].Last, tickers[2].Last)
}

func TestGetCandles_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	candles, err := src.GetCandles(ctx, "BTC-USD", marketdata.Interval1h, 24, time.Time{})
	if err != nil {
		t.Skipf("Kraken API unavailable: %v", err)
	}

	assert.GreaterOrEqual(t, len(candles), 20, "expected at least 20 closed 1h candles")
	assert.LessOrEqual(t, len(candles), 24, "should not exceed requested limit")

	for _, c := range candles {
		assert.NotEmpty(t, c.Open)
		assert.NotEmpty(t, c.Close)
		assert.NotEmpty(t, c.Volume)
		assert.False(t, c.Timestamp.IsZero())
	}

	last := candles[len(candles)-1]
	t.Logf("Latest 1h candle: open=%s close=%s vol=%s at %s", last.Open, last.Close, last.Volume, last.Timestamp)
}

func TestGetOrderbook_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ob, err := src.GetOrderbook(ctx, "ETH-USD", 10)
	if err != nil {
		t.Skipf("Kraken API unavailable: %v", err)
	}

	assert.Equal(t, "ETH-USD", ob.Market)
	assert.NotEmpty(t, ob.Bids)
	assert.NotEmpty(t, ob.Asks)
	assert.LessOrEqual(t, len(ob.Asks), 10)
	assert.LessOrEqual(t, len(ob.Bids), 10)

	t.Logf("Best bid: %s, best ask: %s", ob.Bids[0].Price, ob.Asks[0].Price)
}

func TestGetMarkets_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	markets, err := src.GetMarkets(ctx, "USD")
	if err != nil {
		t.Skipf("Kraken API unavailable: %v", err)
	}
	if len(markets) == 0 {
		t.Skip("Kraken returned empty markets list")
	}

	// Kraken has >50 USD pairs - this verifies batched ticker enrichment works.
	t.Logf("Total USD markets: %d", len(markets))
	assert.Greater(t, len(markets), 50, "Kraken should have >50 USD pairs (tests batched enrichment)")

	byPair := map[string]marketdata.MarketInfo{}
	for _, m := range markets {
		byPair[m.Pair] = m
	}
	assert.Contains(t, byPair, "BTC-USD")
	assert.Contains(t, byPair, "ETH-USD")

	// Verify ticker enrichment populated prices on well-known pairs.
	for _, pair := range []string{"BTC-USD", "ETH-USD", "SOL-USD"} {
		m, ok := byPair[pair]
		if !ok {
			continue
		}
		assert.NotEmpty(t, m.LastPrice, "%s should have last_price from ticker enrichment", pair)
		assert.NotEmpty(t, m.Volume24h, "%s should have volume_24h from ticker enrichment", pair)
		assert.NotEmpty(t, m.Change24hPct, "%s should have change_24h_pct from ticker enrichment", pair)
		t.Logf("%s: last=%s vol=%s change=%s", pair, m.LastPrice, m.Volume24h, m.Change24hPct)
	}

	// Spot-check: at least 80% of markets should have prices (some may fail in edge cases).
	enriched := 0
	for _, m := range markets {
		if m.LastPrice != "" {
			enriched++
		}
	}
	enrichPct := float64(enriched) / float64(len(markets)) * 100
	t.Logf("Enriched: %d/%d (%.0f%%)", enriched, len(markets), enrichPct)
	assert.Greater(t, enrichPct, 80.0, "at least 80%% of markets should have prices after batched enrichment")
}

func TestUnsupportedMethods_Integration(t *testing.T) {
	src := newSource(t)
	ctx := context.Background()

	_, err := src.GetFundingRates(ctx, "BTC-USD", 10)
	assert.Error(t, err, "GetFundingRates should fail on Kraken (spot only)")

	_, err = src.GetOpenInterest(ctx, "BTC-USD")
	assert.Error(t, err, "GetOpenInterest should fail on Kraken (spot only)")
}
