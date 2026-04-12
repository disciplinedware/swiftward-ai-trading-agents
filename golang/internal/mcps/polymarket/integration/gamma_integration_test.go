//go:build integration
// +build integration

// Integration tests for the Polymarket Gamma and CLOB API clients.
// These tests hit the real Polymarket public APIs (no auth required).
// Run with: go test -tags=integration ./internal/mcps/polymarket/integration/...
//
// All tests skip gracefully on network failure so they never break CI.
package integration

import (
	"context"
	"testing"
	"time"

	"ai-trading-agents/internal/mcps/polymarket"
)

func TestGammaListMarkets(t *testing.T) {
	c := polymarket.NewGammaClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	markets, err := c.ListMarkets(ctx, polymarket.ListMarketsParams{Limit: 5})
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	if len(markets) == 0 {
		t.Fatal("expected at least one market")
	}

	for _, m := range markets {
		if len(m.Outcomes) == 0 {
			t.Errorf("market %s: empty Outcomes (schema mismatch?)", m.ID)
		}
		if len(m.OutcomePrices) == 0 {
			t.Errorf("market %s: empty OutcomePrices (schema mismatch?)", m.ID)
		}
		if len(m.ClobTokenIDs) == 0 {
			t.Errorf("market %s: empty ClobTokenIDs (schema mismatch?)", m.ID)
		}
		if m.Volume24hr == 0 {
			t.Logf("market %s: Volume24hr is zero (may be a new market)", m.ID)
		}
	}
	t.Logf("decoded %d markets; first: %q odds=%v", len(markets), markets[0].Question, markets[0].OutcomePrices)
}

func TestGammaGetMarket(t *testing.T) {
	c := polymarket.NewGammaClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// First get a valid ID from list, then fetch it individually.
	markets, err := c.ListMarkets(ctx, polymarket.ListMarketsParams{Limit: 1})
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	if len(markets) == 0 {
		t.Skip("no markets returned")
	}

	id := markets[0].ID
	m, err := c.GetMarket(ctx, id)
	if err != nil {
		t.Fatalf("GetMarket(%s): %v", id, err)
	}
	if m.ID != id {
		t.Errorf("got ID %s, want %s", m.ID, id)
	}
	if len(m.Outcomes) == 0 {
		t.Error("empty Outcomes on single market fetch")
	}
}

func TestCLOBGetBook(t *testing.T) {
	gamma := polymarket.NewGammaClient("")
	clob := polymarket.NewCLOBClient("")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	markets, err := gamma.ListMarkets(ctx, polymarket.ListMarketsParams{Limit: 3})
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}

	for _, m := range markets {
		if len(m.ClobTokenIDs) == 0 {
			continue
		}
		book, err := clob.GetBook(ctx, m.ClobTokenIDs[0])
		if err != nil {
			t.Logf("GetBook for %s: %v (skipping)", m.ID, err)
			continue
		}
		t.Logf("market %s: %d bids, %d asks", m.ID, len(book.Bids), len(book.Asks))
		return
	}
	t.Skip("no markets with CLOB tokens available")
}
