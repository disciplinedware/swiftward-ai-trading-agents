//go:build integration

// Integration tests for the Polymarket search_markets tool against the real Gamma API.
// Run with: go test -tags=integration -v ./internal/mcps/polymarket/integration/...
package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"ai-trading-agents/internal/mcps/polymarket"
)

func newTestService() *polymarket.Service {
	return polymarket.NewTestService(
		zap.Must(zap.NewDevelopment()),
		polymarket.NewGammaClient(""),
		polymarket.NewCLOBClient(""),
	)
}

func TestSearchMarkets_NoFilter_ReturnsEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svc := newTestService()
	result, err := svc.ToolSearchMarkets(ctx, map[string]any{})
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	text := result.Content[0].Text
	if strings.Contains(text, "No markets found") {
		t.Fatalf("default search returned zero results.\nOutput: %s", text)
	}
	if count := strings.Count(text, "EVENT:"); count < 3 {
		t.Errorf("expected at least 3 events, got %d.\nOutput: %s", count, text)
	}
	// Each event should have at least one market with market_id
	if !strings.Contains(text, "market_id:") {
		t.Error("no market_id found in output")
	}
	t.Logf("default search:\n%s", text[:min(len(text), 500)])
}

func TestSearchMarkets_CryptoCategory_ReturnsCryptoEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svc := newTestService()
	result, err := svc.ToolSearchMarkets(ctx, map[string]any{"category": "Crypto"})
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	text := result.Content[0].Text
	if strings.Contains(text, "No markets found") {
		t.Fatalf("crypto search returned zero results.\nOutput: %s", text)
	}

	cryptoKeywords := []string{"btc", "bitcoin", "eth", "ethereum", "crypto", "token", "solana", "sol"}
	lower := strings.ToLower(text)
	found := false
	for _, kw := range cryptoKeywords {
		if strings.Contains(lower, kw) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("crypto search returned no crypto-related content.\nOutput: %s", text)
	}
	t.Logf("crypto search:\n%s", text[:min(len(text), 500)])
}

func TestSearchMarkets_TextQuery_FindsBitcoin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	svc := newTestService()
	result, err := svc.ToolSearchMarkets(ctx, map[string]any{"query": "Bitcoin"})
	if err != nil {
		t.Skipf("network unavailable: %v", err)
	}
	text := result.Content[0].Text
	if strings.Contains(text, "No markets found") {
		t.Fatalf("query 'Bitcoin' returned zero results.\nOutput: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "bitcoin") {
		t.Errorf("results don't mention Bitcoin.\nOutput: %s", text)
	}
	t.Logf("Bitcoin search:\n%s", text[:min(len(text), 500)])
}
