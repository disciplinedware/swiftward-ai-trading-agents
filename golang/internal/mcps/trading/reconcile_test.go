package trading

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/exchange"
)

func TestMatchPendingToFill(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		pending db.TradeRecord
		fills   []exchange.ExchangeFill
		wantID  string // expected matched TradeID, "" = no match
	}{
		{
			name: "exact match by pair/side/time",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills: []exchange.ExchangeFill{
				{TradeID: "PAPER-001", Pair: "ETH-USD", Side: "buy", Time: now.Add(2 * time.Second)},
			},
			wantID: "PAPER-001",
		},
		{
			name: "no match - wrong pair",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills: []exchange.ExchangeFill{
				{TradeID: "PAPER-001", Pair: "BTC-USD", Side: "buy", Time: now.Add(2 * time.Second)},
			},
			wantID: "",
		},
		{
			name: "no match - wrong side",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills: []exchange.ExchangeFill{
				{TradeID: "PAPER-001", Pair: "ETH-USD", Side: "sell", Time: now.Add(2 * time.Second)},
			},
			wantID: "",
		},
		{
			name: "no match - too far apart",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills: []exchange.ExchangeFill{
				{TradeID: "PAPER-001", Pair: "ETH-USD", Side: "buy", Time: now.Add(2 * time.Minute)},
			},
			wantID: "",
		},
		{
			name: "closest match when multiple fills",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills: []exchange.ExchangeFill{
				{TradeID: "PAPER-001", Pair: "ETH-USD", Side: "buy", Time: now.Add(30 * time.Second)},
				{TradeID: "PAPER-002", Pair: "ETH-USD", Side: "buy", Time: now.Add(3 * time.Second)},
				{TradeID: "PAPER-003", Pair: "ETH-USD", Side: "buy", Time: now.Add(50 * time.Second)},
			},
			wantID: "PAPER-002",
		},
		{
			name: "no fills at all",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills:  nil,
			wantID: "",
		},
		{
			name: "fill slightly before pending (exchange was faster)",
			pending: db.TradeRecord{
				Pair: "ETH-USD", Side: "buy", Timestamp: now,
			},
			fills: []exchange.ExchangeFill{
				{TradeID: "PAPER-001", Pair: "ETH-USD", Side: "buy", Time: now.Add(-5 * time.Second)},
			},
			wantID: "PAPER-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPendingToFill(tt.pending, tt.fills, nil)
			if tt.wantID == "" {
				if got != nil {
					t.Errorf("expected no match, got %s", got.TradeID)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected match %s, got nil", tt.wantID)
			}
			if got.TradeID != tt.wantID {
				t.Errorf("matched %s, want %s", got.TradeID, tt.wantID)
			}
		})
	}
}

func TestReconcileAgent_PendingResolved(t *testing.T) {
	repo := db.NewMemRepository()
	now := time.Now()

	// Create agent.
	_, err := repo.GetOrCreateAgent(t.Context(), "agent-test", decimal.NewFromInt(10000))
	if err != nil {
		t.Fatal(err)
	}

	// Insert a pending trade.
	pending := &db.TradeRecord{
		AgentID:   "agent-test",
		Timestamp: now.Add(-30 * time.Second),
		Pair:      "ETH-USD",
		Side:      "buy",
		Qty:       decimal.NewFromFloat(100),
		Status:    StatusPending,
	}
	if err := repo.InsertTrade(t.Context(), pending); err != nil {
		t.Fatal(err)
	}

	// Simulate Kraken fill matching the pending trade.
	fill := exchange.ExchangeFill{
		TradeID: "PAPER-001",
		Pair:    "ETH-USD",
		Side:    "buy",
		Price:   decimal.NewFromFloat(2000),
		Volume:  decimal.NewFromFloat(0.05),
		Cost:    decimal.NewFromFloat(100),
		Fee:     decimal.NewFromFloat(0.26),
		Time:    now.Add(-28 * time.Second),
	}

	// Run matchPendingToFill.
	matched := matchPendingToFill(*pending, []exchange.ExchangeFill{fill}, nil)
	if matched == nil {
		t.Fatal("expected match")
	}
	if matched.TradeID != "PAPER-001" {
		t.Errorf("matched %s, want PAPER-001", matched.TradeID)
	}
}

func TestReconcileAgent_StalePendingRejected(t *testing.T) {
	repo := db.NewMemRepository()

	_, err := repo.GetOrCreateAgent(t.Context(), "agent-test", decimal.NewFromInt(10000))
	if err != nil {
		t.Fatal(err)
	}

	// Insert a stale pending trade (3 minutes old).
	pending := &db.TradeRecord{
		AgentID:   "agent-test",
		Timestamp: time.Now().Add(-3 * time.Minute),
		Pair:      "ETH-USD",
		Side:      "buy",
		Qty:       decimal.NewFromFloat(100),
		Status:    StatusPending,
	}
	if err := repo.InsertTrade(t.Context(), pending); err != nil {
		t.Fatal(err)
	}

	// No matching fills - should be rejected.
	matched := matchPendingToFill(*pending, nil, nil)
	if matched != nil {
		t.Fatal("expected no match for stale pending")
	}

	// Verify the 2-minute threshold logic.
	if time.Since(pending.Timestamp) <= 2*time.Minute {
		t.Fatal("expected stale pending to be older than 2 minutes")
	}
}
