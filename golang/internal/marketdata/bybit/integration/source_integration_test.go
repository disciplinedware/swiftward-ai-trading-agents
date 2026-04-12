//go:build integration
// +build integration

// Integration tests for the Bybit market data source.
// These tests hit the real Bybit public API (no API key required).
// Run with: go test -tags=integration ./internal/marketdata/bybit/integration/...
package integration

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata/bybit"
)

func newSource(t *testing.T) *bybit.Source {
	t.Helper()
	log, _ := zap.NewDevelopment()
	return bybit.NewSource(bybit.Config{}, log)
}

func TestGetOpenInterest_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	oi, err := src.GetOpenInterest(ctx, "ETH-USDC")
	if err != nil {
		t.Skipf("GetOpenInterest failed (may be geo-blocked or API changed): %v", err)
	}

	if oi.Market != "ETH-USDC" {
		t.Errorf("Market: want ETH-USDC got %q", oi.Market)
	}
	if oi.OpenInterest == "" || oi.OpenInterest == "0.00" {
		t.Errorf("OpenInterest is empty or zero: %q", oi.OpenInterest)
	}
	t.Logf("OI: %s, 1h: %s%%, 4h: %s%%, 24h: %s%%",
		oi.OpenInterest, oi.OIChange1hPct, oi.OIChange4hPct, oi.OIChange24hPct)
}

func TestGetFundingRates_Integration(t *testing.T) {
	src := newSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data, err := src.GetFundingRates(ctx, "ETH-USDC", 10)
	if err != nil {
		t.Skipf("GetFundingRates failed (may be geo-blocked or API changed): %v", err)
	}

	if data.Market != "ETH-USDC" {
		t.Errorf("Market: want ETH-USDC got %q", data.Market)
	}
	if data.CurrentRate == "" {
		t.Error("CurrentRate is empty")
	}
	if len(data.History) == 0 {
		t.Error("History is empty")
	}
	t.Logf("Funding rate: %s (annualized: %s%%), history: %d entries",
		data.CurrentRate, data.AnnualizedPct, len(data.History))
}
