//go:build integration
// +build integration

// Integration tests for the PRISM market intelligence API.
// These tests hit the real PRISM API (requires API key).
// Run with: go test -tags=integration ./internal/marketdata/prism/integration/...
//
// Requires: PRISM_API_KEY env var set to a valid key.
// All tests skip gracefully on network failure or missing key.
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata/prism"
)

func newClient(t *testing.T) *prism.Client {
	t.Helper()
	apiKey := os.Getenv("PRISM_API_KEY")
	if apiKey == "" {
		t.Skip("PRISM_API_KEY not set - skipping PRISM integration tests")
	}
	log, _ := zap.NewDevelopment()
	return prism.NewClient(prism.Config{
		BaseURL:          "https://api.prismapi.ai",
		APIKey:           apiKey,
		Timeout:          15 * time.Second,
		FailureThreshold: 3,
		Cooldown:         60 * time.Second,
	}, log)
}

func TestGetFearGreed_Integration(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := c.GetFearGreed(ctx)
	if err != nil {
		t.Skipf("PRISM API unavailable: %v", err)
	}

	assert.GreaterOrEqual(t, resp.Value, 0)
	assert.LessOrEqual(t, resp.Value, 100)
	assert.NotEmpty(t, resp.Label)
	assert.Contains(t, []string{"Extreme Fear", "Fear", "Neutral", "Greed", "Extreme Greed"}, resp.Label)

	t.Logf("Fear & Greed: %d (%s)", resp.Value, resp.Label)
}

func TestGetTechnicals_Integration(t *testing.T) {
	tests := []struct {
		symbol string
	}{
		{"BTC"},
		{"ETH"},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			c := newClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resp, err := c.GetTechnicals(ctx, tt.symbol)
			if err != nil {
				t.Skipf("PRISM API unavailable for %s: %v", tt.symbol, err)
			}

			assert.Equal(t, tt.symbol, resp.Symbol)
			assert.NotEmpty(t, resp.Timeframe)
			require.NotNil(t, resp.RSI, "RSI should be present")
			assert.Greater(t, *resp.RSI, 0.0)
			assert.Less(t, *resp.RSI, 100.0)
			assert.NotEmpty(t, resp.OverallSignal)
			assert.Contains(t, []string{"bullish", "bearish", "neutral"}, resp.OverallSignal)

			var price float64
			if resp.CurrentPrice != nil {
				price = *resp.CurrentPrice
			}
			t.Logf("%s: RSI=%.1f MACD_trend=%s overall=%s price=%.2f",
				tt.symbol, *resp.RSI, resp.MACDTrend, resp.OverallSignal, price)
		})
	}
}

func TestGetSignalsSummary_Integration(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := c.GetSignalsSummary(ctx, []string{"BTC", "ETH"})
	if err != nil {
		t.Skipf("PRISM API unavailable: %v", err)
	}

	assert.GreaterOrEqual(t, len(resp.Data), 1, "should have at least 1 signal entry")

	for _, entry := range resp.Data {
		assert.NotEmpty(t, entry.Symbol)
		assert.NotEmpty(t, entry.OverallSignal)
		assert.Contains(t, []string{"strong_bullish", "bullish", "neutral", "bearish", "strong_bearish"}, entry.OverallSignal)
		assert.NotEmpty(t, entry.Direction)
		t.Logf("%s: signal=%s direction=%s strength=%s net_score=%d active_signals=%d",
			entry.Symbol, entry.OverallSignal, entry.Direction, entry.Strength,
			entry.NetScore, entry.SignalCount)
	}

	assert.GreaterOrEqual(t, resp.Summary.Total, 1)
}

func TestCanonicalSymbolMapping_Integration(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Verify our canonical-to-base mapping works with the real API.
	symbol := prism.CanonicalToBase("ETH-USDC")
	assert.Equal(t, "ETH", symbol)

	resp, err := c.GetTechnicals(ctx, symbol)
	if err != nil {
		t.Skipf("PRISM API unavailable: %v", err)
	}
	assert.Equal(t, "ETH", resp.Symbol)
}

func TestCircuitBreakerRecovery_Integration(t *testing.T) {
	// Verify that a healthy client stays in closed state across multiple calls.
	c := newClient(t)

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := c.GetFearGreed(ctx)
		cancel()
		if err != nil {
			t.Skipf("PRISM API unavailable on call %d: %v", i+1, err)
		}
		assert.Equal(t, "closed", c.State(), "circuit breaker should stay closed on success")
	}
}
