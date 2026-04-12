package db

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestRepository runs the full repository test suite against MemRepository.
// The same tests can be run against PgxRepository by creating it with a test database.
func TestRepository(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	t.Run("GetOrCreateAgent", func(t *testing.T) {
		tests := []struct {
			name        string
			agentID     string
			initialCash decimal.Decimal
			wantCash    decimal.Decimal
		}{
			{"new agent", "agent-1", decimal.NewFromInt(10000), decimal.NewFromInt(10000)},
			{"idempotent create", "agent-1", decimal.NewFromInt(99999), decimal.NewFromInt(10000)}, // should return existing, not overwrite
			{"different agent", "agent-2", decimal.NewFromInt(5000), decimal.NewFromInt(5000)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				state, err := repo.GetOrCreateAgent(ctx, tt.agentID, tt.initialCash)
				if err != nil {
					t.Fatal(err)
				}
				if !state.Cash.Equal(tt.wantCash) {
					t.Errorf("cash = %s, want %s", state.Cash, tt.wantCash)
				}
				if state.AgentID != tt.agentID {
					t.Errorf("agentID = %s, want %s", state.AgentID, tt.agentID)
				}
			})
		}
	})

	t.Run("UpdateAgentState", func(t *testing.T) {
		state, _ := repo.GetAgent(ctx, "agent-1")
		state.Cash = decimal.NewFromInt(8000)
		state.FillCount = 5
		state.PeakValue = decimal.NewFromInt(11000)

		if err := repo.UpdateAgentState(ctx, state); err != nil {
			t.Fatal(err)
		}

		got, _ := repo.GetAgent(ctx, "agent-1")
		if !got.Cash.Equal(decimal.NewFromInt(8000)) || got.FillCount != 5 || !got.PeakValue.Equal(decimal.NewFromInt(11000)) {
			t.Errorf("state not updated: cash=%s trades=%d peak=%s", got.Cash, got.FillCount, got.PeakValue)
		}
	})

	t.Run("InsertAndGetTrades", func(t *testing.T) {
		now := time.Now()
		trades := []TradeRecord{
			{AgentID: "agent-1", Timestamp: now.Add(-2 * time.Minute), Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromFloat(0.04), Price: decimal.NewFromInt(2500), Value: decimal.NewFromInt(100), Status: "fill", ValueAfter: decimal.NewFromInt(10000)},
			{AgentID: "agent-1", Timestamp: now.Add(-1 * time.Minute), Pair: "BTC-USDC", Side: "buy", Qty: decimal.NewFromFloat(0.001), Price: decimal.NewFromInt(65000), Value: decimal.NewFromInt(65), Status: "fill", ValueAfter: decimal.NewFromInt(9950)},
			{AgentID: "agent-1", Timestamp: now, Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromFloat(0.02), Price: decimal.NewFromInt(2510), Value: decimal.NewFromInt(50), Status: "reject"},
			{AgentID: "agent-2", Timestamp: now, Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromFloat(0.1), Price: decimal.NewFromInt(2500), Value: decimal.NewFromInt(250), Status: "fill", ValueAfter: decimal.NewFromInt(5000)},
		}
		for i := range trades {
			if err := repo.InsertTrade(ctx, &trades[i]); err != nil {
				t.Fatal(err)
			}
		}

		tests := []struct {
			name      string
			agentID   string
			limit     int
			market    string
			verdict   string
			wantCount int
		}{
			{"all agent-1", "agent-1", 0, "", "", 3},
			{"agent-1 limit 1", "agent-1", 1, "", "", 1},
			{"agent-1 ETH only", "agent-1", 0, "ETH-USDC", "", 2},
			{"agent-1 fill only", "agent-1", 0, "", "fill", 2},
			{"agent-1 reject only", "agent-1", 0, "", "reject", 1},
			{"agent-2 all", "agent-2", 0, "", "", 1},
			{"nonexistent agent", "agent-99", 0, "", "", 0},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := repo.GetTradeHistory(ctx, tt.agentID, tt.limit, tt.market, tt.verdict)
				if err != nil {
					t.Fatal(err)
				}
				if len(got) != tt.wantCount {
					t.Errorf("got %d trades, want %d", len(got), tt.wantCount)
				}
			})
		}
	})

	t.Run("GetOpenPositions", func(t *testing.T) {
		positions, err := repo.GetOpenPositions(ctx, "agent-1")
		if err != nil {
			t.Fatal(err)
		}

		// agent-1 has buys in ETH-USDC (0.04 + 0.02 but 0.02 is rejected) and BTC-USDC (0.001)
		// Only approved trades count, so ETH = 0.04, BTC = 0.001
		posMap := make(map[string]OpenPosition)
		for _, p := range positions {
			posMap[p.Pair] = p
		}

		if eth, ok := posMap["ETH-USDC"]; !ok {
			t.Error("missing ETH-USDC position")
		} else {
			diff := eth.Qty.Sub(decimal.NewFromFloat(0.04)).Abs()
			if diff.GreaterThan(decimal.NewFromFloat(0.001)) {
				t.Errorf("ETH size = %s, want ~0.04", eth.Qty)
			}
		}

		if btc, ok := posMap["BTC-USDC"]; !ok {
			t.Error("missing BTC-USDC position")
		} else {
			diff := btc.Qty.Sub(decimal.NewFromFloat(0.001)).Abs()
			if diff.GreaterThan(decimal.NewFromFloat(0.0001)) {
				t.Errorf("BTC size = %s, want ~0.001", btc.Qty)
			}
		}
	})

	t.Run("GetOpenPositions_NetZero", func(t *testing.T) {
		// Create an agent with a buy and sell that cancel out
		if _, err := repo.GetOrCreateAgent(ctx, "agent-flat", decimal.NewFromInt(10000)); err != nil {
			t.Fatal(err)
		}
		if err := repo.InsertTrade(ctx, &TradeRecord{
			AgentID: "agent-flat", Timestamp: time.Now().Add(-time.Minute),
			Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromFloat(0.1), Price: decimal.NewFromInt(2500), Value: decimal.NewFromInt(250), Status: "fill",
		}); err != nil {
			t.Fatal(err)
		}
		if err := repo.InsertTrade(ctx, &TradeRecord{
			AgentID: "agent-flat", Timestamp: time.Now(),
			Pair: "ETH-USDC", Side: "sell", Qty: decimal.NewFromFloat(0.1), Price: decimal.NewFromInt(2600), Value: decimal.NewFromInt(260), Status: "fill",
		}); err != nil {
			t.Fatal(err)
		}

		positions, err := repo.GetOpenPositions(ctx, "agent-flat")
		if err != nil {
			t.Fatal(err)
		}
		if len(positions) != 0 {
			t.Errorf("expected 0 positions after full close, got %d", len(positions))
		}
	})

	t.Run("GetOpenPositions_PartialSellCostBasis", func(t *testing.T) {
		// Buy 1.0 ETH at $2000, then sell 0.5 ETH at $3000
		// AvgPrice should still be $2000 (buy avg), not distorted by sell proceeds
		if _, err := repo.GetOrCreateAgent(ctx, "agent-partial", decimal.NewFromInt(10000)); err != nil {
			t.Fatal(err)
		}
		if err := repo.InsertTrade(ctx, &TradeRecord{
			AgentID: "agent-partial", Timestamp: time.Now().Add(-2 * time.Minute),
			Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(2000), Value: decimal.NewFromInt(2000), Status: "fill",
		}); err != nil {
			t.Fatal(err)
		}
		if err := repo.InsertTrade(ctx, &TradeRecord{
			AgentID: "agent-partial", Timestamp: time.Now().Add(-time.Minute),
			Pair: "ETH-USDC", Side: "sell", Qty: decimal.NewFromFloat(0.5), Price: decimal.NewFromInt(3000), Value: decimal.NewFromInt(1500), Status: "fill",
		}); err != nil {
			t.Fatal(err)
		}

		positions, err := repo.GetOpenPositions(ctx, "agent-partial")
		if err != nil {
			t.Fatal(err)
		}
		if len(positions) != 1 {
			t.Fatalf("expected 1 position, got %d", len(positions))
		}
		pos := positions[0]
		assertDecimalApprox(t, "Qty", pos.Qty, decimal.NewFromFloat(0.5), decimal.NewFromFloat(0.01))
		// AvgPrice must reflect buy price, not be distorted by profitable sell
		assertDecimalApprox(t, "AvgPrice", pos.AvgPrice, decimal.NewFromInt(2000), decimal.NewFromInt(10))
		// CostUSD = remaining qty * avg buy price = 0.5 * 2000 = 1000
		assertDecimalApprox(t, "Value", pos.Value, decimal.NewFromInt(1000), decimal.NewFromInt(10))
	})

	t.Run("GetOpenPositions_CloseAndReopen", func(t *testing.T) {
		// Buy 1 @100, sell 1, buy 1 @200 => avg should be 200, not 150
		if _, err := repo.GetOrCreateAgent(ctx, "agent-reopen", decimal.NewFromInt(10000)); err != nil {
			t.Fatal(err)
		}
		trades := []TradeRecord{
			{AgentID: "agent-reopen", Timestamp: time.Now().Add(-3 * time.Minute), Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(100), Status: "fill"},
			{AgentID: "agent-reopen", Timestamp: time.Now().Add(-2 * time.Minute), Pair: "ETH-USDC", Side: "sell", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(150), Value: decimal.NewFromInt(150), Status: "fill"},
			{AgentID: "agent-reopen", Timestamp: time.Now().Add(-1 * time.Minute), Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(200), Value: decimal.NewFromInt(200), Status: "fill"},
		}
		for i := range trades {
			if err := repo.InsertTrade(ctx, &trades[i]); err != nil {
				t.Fatal(err)
			}
		}

		positions, err := repo.GetOpenPositions(ctx, "agent-reopen")
		if err != nil {
			t.Fatal(err)
		}
		if len(positions) != 1 {
			t.Fatalf("expected 1 position, got %d", len(positions))
		}
		pos := positions[0]
		assertDecimalApprox(t, "AvgPrice", pos.AvgPrice, decimal.NewFromInt(200), decimal.NewFromInt(5))
		assertDecimalApprox(t, "Value", pos.Value, decimal.NewFromInt(200), decimal.NewFromInt(5))
	})

	t.Run("ComputeEquity", func(t *testing.T) {
		// agent-1: cash=8000 (from UpdateAgentState above), ETH pos ~0.04 @ ~2500, BTC pos ~0.001 @ ~65000
		prices := map[string]decimal.Decimal{"ETH-USDC": decimal.NewFromInt(2500), "BTC-USDC": decimal.NewFromInt(65000)}
		equity, err := repo.ComputeEquity(ctx, "agent-1", prices)
		if err != nil {
			t.Fatal(err)
		}
		// 8000 + 0.04*2500 + 0.001*65000 = 8000 + 100 + 65 = 8165
		if equity.LessThan(decimal.NewFromInt(8100)) || equity.GreaterThan(decimal.NewFromInt(8250)) {
			t.Errorf("equity = %s, expected ~8165", equity)
		}
	})

	t.Run("ComputeEquity_MissingPrice", func(t *testing.T) {
		// When a price is missing, should fall back to CostUSD
		prices := map[string]decimal.Decimal{"ETH-USDC": decimal.NewFromInt(2500)} // no BTC price
		equity, err := repo.ComputeEquity(ctx, "agent-1", prices)
		if err != nil {
			t.Fatal(err)
		}
		// 8000 + 0.04*2500 + BTC costUSD = 8000 + 100 + 65 = 8165
		if equity.LessThan(decimal.NewFromInt(8100)) || equity.GreaterThan(decimal.NewFromInt(8200)) {
			t.Errorf("equity = %s, expected ~8165 (CostUSD fallback for BTC)", equity)
		}
	})

	t.Run("ComputeMetrics", func(t *testing.T) {
		prices := map[string]decimal.Decimal{"ETH-USDC": decimal.NewFromInt(2500), "BTC-USDC": decimal.NewFromInt(65000)}
		metrics, err := repo.ComputeMetrics(ctx, "agent-1", prices)
		if err != nil {
			t.Fatal(err)
		}

		// Basic sanity checks
		if metrics.TotalReturnPct == 0 && metrics.MaxDrawdownPct == 0 && metrics.CompliancePct == 0 {
			t.Error("all metrics zero - expected some values")
		}
		if metrics.CompliancePct < 0 || metrics.CompliancePct > 100 {
			t.Errorf("compliance = %.1f%%, want 0-100", metrics.CompliancePct)
		}
	})

	t.Run("ListAgents", func(t *testing.T) {
		listRepo := NewMemRepository()
		// Empty repo: empty slice, no error.
		got, err := listRepo.ListAgents(ctx)
		if err != nil {
			t.Fatalf("empty list: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("empty list len = %d, want 0", len(got))
		}

		// Inserting in non-sorted order should still come back sorted by ID.
		ids := []string{"agent-zeta", "agent-alpha", "agent-mid"}
		for _, id := range ids {
			if _, err := listRepo.GetOrCreateAgent(ctx, id, decimal.NewFromInt(10000)); err != nil {
				t.Fatalf("create %s: %v", id, err)
			}
		}

		got, err = listRepo.ListAgents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wantOrder := []string{"agent-alpha", "agent-mid", "agent-zeta"}
		if len(got) != len(wantOrder) {
			t.Fatalf("len = %d, want %d", len(got), len(wantOrder))
		}
		for i, w := range wantOrder {
			if got[i].AgentID != w {
				t.Errorf("[%d] = %s, want %s", i, got[i].AgentID, w)
			}
		}

		// Mutating returned state must not affect the repo (defensive copy).
		got[0].Cash = decimal.NewFromInt(-1)
		fresh, _ := listRepo.GetAgent(ctx, "agent-alpha")
		if !fresh.Cash.Equal(decimal.NewFromInt(10000)) {
			t.Errorf("ListAgents leaked mutable state: cash = %s", fresh.Cash)
		}
	})

	t.Run("GetAgent_NotFound", func(t *testing.T) {
		_, err := repo.GetAgent(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent agent")
		}
	})

	t.Run("UpdateAgentState_NotFound", func(t *testing.T) {
		err := repo.UpdateAgentState(ctx, &AgentState{AgentID: "nonexistent"})
		if err == nil {
			t.Error("expected error for nonexistent agent")
		}
	})

	t.Run("GetFilledTradesPendingAttestation", func(t *testing.T) {
		// Fresh agent so we don't collide with trades from earlier subtests.
		agent := "attest-agent"
		_, _ = repo.GetOrCreateAgent(ctx, agent, decimal.NewFromInt(10000))

		now := time.Now()
		cases := []struct {
			status   string
			attState string // "" = no attestation key at all (null state)
		}{
			{"fill", ""},                // null attestation - YES, recoverable (crash-in-window case)
			{"fill", "pending"},         // YES
			{"fill", "pending_confirm"}, // YES - tx landed but success DB write failed; finalize without chain call
			{"fill", "waiting_for_gas"}, // YES
			{"fill", "success"},         // NO - terminal
			{"fill", "error"},           // NO - terminal
			{"fill", "disabled"},        // NO - terminal
			{"reject", "pending"},       // NO - wrong trade status
		}
		for i, c := range cases {
			rec := TradeRecord{
				AgentID:    agent,
				Timestamp:  now.Add(time.Duration(i) * time.Second),
				Pair:       "ETH-USD",
				Side:       "buy",
				Qty:        decimal.NewFromFloat(0.01),
				Price:      decimal.NewFromInt(3000),
				Value:      decimal.NewFromInt(30),
				Status:     c.status,
				ValueAfter: decimal.NewFromInt(10000),
			}
			if err := repo.InsertTrade(ctx, &rec); err != nil {
				t.Fatalf("insert %d: %v", i, err)
			}
			if c.attState != "" {
				att := map[string]any{"status": c.attState}
				if err := repo.UpdateEvidence(ctx, rec.ID, map[string]any{"attestation": att}); err != nil {
					t.Fatalf("update evidence %d: %v", i, err)
				}
			}
		}

		got, err := repo.GetFilledTradesPendingAttestation(ctx, agent)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 4 {
			t.Fatalf("got %d recoverable trades, want 4 (null + pending + pending_confirm + waiting_for_gas)", len(got))
		}
		// Verify the four returned are the null + pending + pending_confirm + waiting_for_gas ones.
		// Null-attestation trades are represented by an empty status string.
		statuses := map[string]bool{}
		for _, tr := range got {
			att, _ := tr.Evidence["attestation"].(map[string]any)
			s, _ := att["status"].(string)
			statuses[s] = true
		}
		if !statuses[""] || !statuses["pending"] || !statuses["pending_confirm"] || !statuses["waiting_for_gas"] {
			t.Errorf("returned statuses = %v, want {<null>, pending, pending_confirm, waiting_for_gas}", statuses)
		}
	})
}

func TestComputeMetricsFromTrades(t *testing.T) {
	tests := []struct {
		name           string
		trades         []TradeRecord
		state          *AgentState
		equity         decimal.Decimal
		wantReturn     float64 // approximate
		wantDrawdown   float64 // <= 0
		wantWinRate    float64
		wantSharpeSign int // -1, 0, 1
		wantCompliance float64
	}{
		{
			name:   "no trades",
			trades: nil,
			state:  &AgentState{InitialValue: decimal.NewFromInt(10000), FillCount: 0, RejectCount: 0},
			equity: decimal.NewFromInt(10000),
			// All zeros
			wantReturn:     0,
			wantDrawdown:   0,
			wantWinRate:    0,
			wantSharpeSign: 0,
			wantCompliance: 0, // 0/0
		},
		{
			name: "profitable sells",
			trades: []TradeRecord{
				{Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(100), ValueAfter: decimal.NewFromInt(10000)},
				{Side: "sell", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(110), Value: decimal.NewFromInt(110), PnL: decimal.NewFromInt(10), ValueAfter: decimal.NewFromInt(10010)},
				{Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(100), ValueAfter: decimal.NewFromInt(10010)},
				{Side: "sell", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(120), Value: decimal.NewFromInt(120), PnL: decimal.NewFromInt(20), ValueAfter: decimal.NewFromInt(10030)},
			},
			state:          &AgentState{InitialValue: decimal.NewFromInt(10000), FillCount: 4, RejectCount: 1},
			equity:         decimal.NewFromInt(10030),
			wantReturn:     0.3,
			wantDrawdown:   0, // monotonically increasing
			wantWinRate:    1.0,
			wantSharpeSign: 1,
			wantCompliance: 80, // 4/(4+1)
		},
		{
			name: "mixed wins and losses with drawdown",
			trades: []TradeRecord{
				{Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(100), ValueAfter: decimal.NewFromInt(10000)},
				{Side: "sell", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(110), Value: decimal.NewFromInt(110), PnL: decimal.NewFromInt(10), ValueAfter: decimal.NewFromInt(10010)},
				{Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(100), ValueAfter: decimal.NewFromInt(10010)},
				{Side: "sell", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(80), Value: decimal.NewFromInt(80), PnL: decimal.NewFromInt(-20), ValueAfter: decimal.NewFromInt(9990)},
				{Side: "buy", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(100), ValueAfter: decimal.NewFromInt(9990)},
				{Side: "sell", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(105), Value: decimal.NewFromInt(105), PnL: decimal.NewFromInt(5), ValueAfter: decimal.NewFromInt(9995)},
			},
			state:          &AgentState{InitialValue: decimal.NewFromInt(10000), FillCount: 6, RejectCount: 0},
			equity:         decimal.NewFromInt(9995),
			wantReturn:     -0.05,
			wantDrawdown:   -1, // should be negative (had a drawdown)
			wantWinRate:    2.0 / 3.0,
			wantSharpeSign: 0, // could be positive or negative, just check it runs
			wantCompliance: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := computeMetricsFromTrades(tt.trades, tt.state, tt.equity)

			if math.Abs(m.TotalReturnPct-tt.wantReturn) > 1 {
				t.Errorf("TotalReturnPct = %.2f, want ~%.2f", m.TotalReturnPct, tt.wantReturn)
			}
			if tt.wantDrawdown < 0 && m.MaxDrawdownPct >= 0 {
				t.Errorf("MaxDrawdownPct = %.2f, want negative", m.MaxDrawdownPct)
			}
			if tt.wantDrawdown == 0 && m.MaxDrawdownPct < -0.01 {
				t.Errorf("MaxDrawdownPct = %.2f, want ~0", m.MaxDrawdownPct)
			}
			if math.Abs(m.WinRate-tt.wantWinRate) > 0.01 {
				t.Errorf("WinRate = %.2f, want %.2f", m.WinRate, tt.wantWinRate)
			}
			if tt.wantSharpeSign > 0 && m.SharpeRatio <= 0 {
				t.Errorf("SharpeRatio = %.2f, want positive", m.SharpeRatio)
			}
			if math.Abs(m.CompliancePct-tt.wantCompliance) > 0.1 {
				t.Errorf("CompliancePct = %.1f, want %.1f", m.CompliancePct, tt.wantCompliance)
			}
		})
	}
}

// TestRecordTradeDeltasNotAbsolute verifies that two RecordTrade calls with
// stale state snapshots don't overwrite each other's increments.
// This simulates the multi-instance scenario where both instances read state
// before either writes.
func TestRecordTradeDeltasNotAbsolute(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	_, err := repo.GetOrCreateAgent(ctx, "agent-delta", decimal.NewFromInt(10000))
	if err != nil {
		t.Fatal(err)
	}

	// Two concurrent trades: each adds 1 to trade_count and changes cash
	update1 := &StateUpdate{
		AgentID:       "agent-delta",
		CashDelta:     decimal.NewFromInt(-1000), // buy
		PeakValue:     decimal.NewFromInt(10000),
		FillCountIncr: 1,
	}
	trade1 := &TradeRecord{
		AgentID:   "agent-delta",
		Timestamp: time.Now(),
		Pair:      "ETH-USDC",
		Side:      "buy",
		Qty:       decimal.NewFromInt(1),
		Price:     decimal.NewFromInt(1000),
		Value:     decimal.NewFromInt(1000),
		Status:    "fill",
	}

	update2 := &StateUpdate{
		AgentID:       "agent-delta",
		CashDelta:     decimal.NewFromInt(-2000), // another buy
		PeakValue:     decimal.NewFromInt(10000),
		FillCountIncr: 1,
	}
	trade2 := &TradeRecord{
		AgentID:   "agent-delta",
		Timestamp: time.Now(),
		Pair:      "BTC-USDC",
		Side:      "buy",
		Qty:       decimal.NewFromFloat(0.05),
		Price:     decimal.NewFromInt(40000),
		Value:     decimal.NewFromInt(2000),
		Status:    "fill",
	}

	// Apply both sequentially (simulating two instances that both read stale state)
	if err := repo.RecordTrade(ctx, update1, trade1); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordTrade(ctx, update2, trade2); err != nil {
		t.Fatal(err)
	}

	state, err := repo.GetAgent(ctx, "agent-delta")
	if err != nil {
		t.Fatal(err)
	}

	// With deltas: 10000 + (-1000) + (-2000) = 7000
	// With absolute writes (old bug): second write would overwrite first
	if !state.Cash.Equal(decimal.NewFromInt(7000)) {
		t.Errorf("Cash = %s, want 7000 (deltas should accumulate, not overwrite)", state.Cash)
	}
	if state.FillCount != 2 {
		t.Errorf("FillCount = %d, want 2", state.FillCount)
	}
}

// TestSetAgentHalted verifies that halt flag persists via SetAgentHalted and
// is readable via GetAgent. This was moved from process-local atomic.Bool to DB.
func TestSetAgentHalted(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	if _, err := repo.GetOrCreateAgent(ctx, "agent-halt", decimal.NewFromInt(10000)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		halted     bool
		wantHalted bool
	}{
		{"halt agent", true, true},
		{"resume agent", false, false},
		{"halt again", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := repo.SetAgentHalted(ctx, "agent-halt", tt.halted); err != nil {
				t.Fatal(err)
			}
			state, err := repo.GetAgent(ctx, "agent-halt")
			if err != nil {
				t.Fatal(err)
			}
			if state.Halted != tt.wantHalted {
				t.Errorf("Halted = %v, want %v", state.Halted, tt.wantHalted)
			}
		})
	}

	// SetAgentHalted on nonexistent agent should error
	if err := repo.SetAgentHalted(ctx, "nonexistent", true); err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

// TestGetTradeHistoryDeterministicOrder verifies that trades with the same
// timestamp are ordered by id (descending) for stable, deterministic results.
// This was a code review fix - without the id tiebreaker, same-timestamp trades
// could appear in any order.
func TestGetTradeHistoryDeterministicOrder(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	if _, err := repo.GetOrCreateAgent(ctx, "agent-order", decimal.NewFromInt(10000)); err != nil {
		t.Fatal(err)
	}

	// Insert 5 trades with the exact same timestamp
	sameTime := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	markets := []string{"ETH-USDC", "BTC-USDC", "SOL-USDC", "AVAX-USDC", "LINK-USDC"}
	for _, market := range markets {
		if err := repo.InsertTrade(ctx, &TradeRecord{
			AgentID:   "agent-order",
			Timestamp: sameTime,
			Pair:      market,
			Side:      "buy",
			Qty:       decimal.NewFromInt(1),
			Price:     decimal.NewFromInt(100),
			Value:     decimal.NewFromInt(100),
			Status:    "fill",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Query twice - results must be identical (deterministic)
	trades1, err := repo.GetTradeHistory(ctx, "agent-order", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	trades2, err := repo.GetTradeHistory(ctx, "agent-order", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if len(trades1) != 5 || len(trades2) != 5 {
		t.Fatalf("expected 5 trades each, got %d and %d", len(trades1), len(trades2))
	}
	for i := range trades1 {
		if trades1[i].Pair != trades2[i].Pair {
			t.Errorf("trade[%d] pair mismatch: %s vs %s (non-deterministic ordering)", i, trades1[i].Pair, trades2[i].Pair)
		}
	}

	// Verify descending order by id (last inserted = highest id = first in result)
	for i := 0; i < len(trades1)-1; i++ {
		if trades1[i].ID < trades1[i+1].ID {
			t.Errorf("trades not in descending id order: trade[%d].ID=%d < trade[%d].ID=%d",
				i, trades1[i].ID, i+1, trades1[i+1].ID)
		}
	}
}

// TestRecordTradeRejectedIncrement verifies that rejected trades correctly
// increment RejectCount via delta-based RecordTrade.
func TestRecordTradeRejectedIncrement(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	if _, err := repo.GetOrCreateAgent(ctx, "agent-rej", decimal.NewFromInt(10000)); err != nil {
		t.Fatal(err)
	}

	update := &StateUpdate{
		AgentID:    "agent-rej",
		CashDelta:  decimal.Zero,
		PeakValue:  decimal.NewFromInt(10000),
		RejectIncr: 1,
	}
	trade := &TradeRecord{
		AgentID:   "agent-rej",
		Timestamp: time.Now(),
		Pair:      "ETH-USDC",
		Side:      "buy",
		Qty:       decimal.NewFromFloat(0.04),
		Status:    "reject",
		Reason:    "exceeds position limit",
	}

	if err := repo.RecordTrade(ctx, update, trade); err != nil {
		t.Fatal(err)
	}

	state, err := repo.GetAgent(ctx, "agent-rej")
	if err != nil {
		t.Fatal(err)
	}
	if state.RejectCount != 1 {
		t.Errorf("RejectCount = %d, want 1", state.RejectCount)
	}
	if state.FillCount != 0 {
		t.Errorf("FillCount = %d, want 0 (rejection should not increment)", state.FillCount)
	}
	if !state.Cash.Equal(decimal.NewFromInt(10000)) {
		t.Errorf("Cash = %s, want 10000 (rejection should not change cash)", state.Cash)
	}
}

// TestRecordTradeCashOverdraw verifies that MemRepository rejects trades
// that would make cash negative, matching the Postgres CHECK (cash >= 0) constraint.
func TestRecordTradeCashOverdraw(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	if _, err := repo.GetOrCreateAgent(ctx, "agent-od", decimal.NewFromInt(100)); err != nil {
		t.Fatal(err)
	}

	update := &StateUpdate{
		AgentID:       "agent-od",
		CashDelta:     decimal.NewFromInt(-200), // overdraw: 100 - 200 = -100
		PeakValue:     decimal.NewFromInt(100),
		FillCountIncr: 1,
	}
	trade := &TradeRecord{
		AgentID:   "agent-od",
		Timestamp: time.Now(),
		Pair:      "ETH-USDC",
		Side:      "buy",
		Qty:       decimal.NewFromInt(1),
		Price:     decimal.NewFromInt(200),
		Value:     decimal.NewFromInt(200),
		Status:    "fill",
	}

	err := repo.RecordTrade(ctx, update, trade)
	if err == nil {
		t.Error("expected error for cash overdraw, got nil")
	}

	// Verify state unchanged
	state, _ := repo.GetAgent(ctx, "agent-od")
	if !state.Cash.Equal(decimal.NewFromInt(100)) {
		t.Errorf("cash = %s, want 100 (unchanged after failed overdraw)", state.Cash)
	}
	if state.FillCount != 0 {
		t.Errorf("fill_count = %d, want 0 (unchanged after failed overdraw)", state.FillCount)
	}
}

// TestDecisionTraces verifies DB-backed evidence chain: insert, get, latest hash,
// and multi-agent independence.
func TestDecisionTraces(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	t.Run("insert_and_get", func(t *testing.T) {
		trace := &DecisionTrace{
			DecisionHash: "0xabc123",
			AgentID:      "agent-1",
			PrevHash:     "0x0000000000000000000000000000000000000000000000000000000000000000",
			TraceJSON:    []byte(`{"pair":"ETH-USDC","side":"buy"}`),
			CreatedAt:    time.Now(),
		}
		if err := repo.InsertTrace(ctx, trace); err != nil {
			t.Fatal(err)
		}

		got, err := repo.GetTrace(ctx, "0xabc123")
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentID != "agent-1" {
			t.Errorf("AgentID = %s, want agent-1", got.AgentID)
		}
		if string(got.TraceJSON) != `{"pair":"ETH-USDC","side":"buy"}` {
			t.Errorf("TraceJSON = %s", got.TraceJSON)
		}
	})

	t.Run("get_nonexistent_returns_error", func(t *testing.T) {
		_, err := repo.GetTrace(ctx, "0xdeadbeef")
		if err == nil {
			t.Error("expected error for nonexistent trace")
		}
		if !errors.Is(err, ErrTraceNotFound) {
			t.Errorf("expected ErrTraceNotFound, got %v", err)
		}
	})

	t.Run("insert_idempotent", func(t *testing.T) {
		trace := &DecisionTrace{
			DecisionHash: "0xdup",
			AgentID:      "agent-1",
			PrevHash:     "0xabc123",
			TraceJSON:    []byte(`{"v":1}`),
			CreatedAt:    time.Now(),
		}
		if err := repo.InsertTrace(ctx, trace); err != nil {
			t.Fatal(err)
		}
		// Insert again - should not error
		if err := repo.InsertTrace(ctx, trace); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("get_latest_trace_hash", func(t *testing.T) {
		hash, err := repo.GetLatestTraceHash(ctx, "agent-1")
		if err != nil {
			t.Fatal(err)
		}
		// Last inserted for agent-1 was "0xdup"
		if hash != "0xdup" {
			t.Errorf("latest hash = %s, want 0xdup", hash)
		}
	})

	t.Run("get_latest_trace_hash_no_traces", func(t *testing.T) {
		hash, err := repo.GetLatestTraceHash(ctx, "agent-no-traces")
		if err != nil {
			t.Errorf("expected nil error for agent with no traces, got %v", err)
		}
		if hash != "" {
			t.Errorf("expected empty hash for agent with no traces, got %q", hash)
		}
	})

	t.Run("multi_agent_independent_chains", func(t *testing.T) {
		t1 := &DecisionTrace{
			DecisionHash: "0xagent2-first",
			AgentID:      "agent-2",
			PrevHash:     "0x0000000000000000000000000000000000000000000000000000000000000000",
			TraceJSON:    []byte(`{"agent":"2"}`),
			CreatedAt:    time.Now(),
		}
		if err := repo.InsertTrace(ctx, t1); err != nil {
			t.Fatal(err)
		}

		// agent-1's latest should still be 0xdup, not affected by agent-2
		h1, err := repo.GetLatestTraceHash(ctx, "agent-1")
		if err != nil {
			t.Fatal(err)
		}
		if h1 != "0xdup" {
			t.Errorf("agent-1 latest = %s, want 0xdup", h1)
		}

		h2, err := repo.GetLatestTraceHash(ctx, "agent-2")
		if err != nil {
			t.Fatal(err)
		}
		if h2 != "0xagent2-first" {
			t.Errorf("agent-2 latest = %s, want 0xagent2-first", h2)
		}
	})
}

// TestInsertTraceLatestByInsertionOrder verifies that the latest trace is always
// the last-inserted one (highest seq), matching pgx's ORDER BY seq DESC behavior.
// CreatedAt is irrelevant for ordering - the BIGSERIAL seq column is the tiebreaker.
func TestInsertTraceLatestByInsertionOrder(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	// Insert first trace
	if err := repo.InsertTrace(ctx, &DecisionTrace{
		DecisionHash: "0xfirst",
		AgentID:      "agent-seq",
		PrevHash:     "0x0",
		TraceJSON:    []byte(`{"seq":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	hash, _ := repo.GetLatestTraceHash(ctx, "agent-seq")
	if hash != "0xfirst" {
		t.Errorf("latest = %s, want 0xfirst", hash)
	}

	// Insert second trace - becomes latest regardless of CreatedAt
	if err := repo.InsertTrace(ctx, &DecisionTrace{
		DecisionHash: "0xsecond",
		AgentID:      "agent-seq",
		PrevHash:     "0xfirst",
		TraceJSON:    []byte(`{"seq":2}`),
	}); err != nil {
		t.Fatal(err)
	}

	hash, _ = repo.GetLatestTraceHash(ctx, "agent-seq")
	if hash != "0xsecond" {
		t.Errorf("latest = %s, want 0xsecond (last inserted wins)", hash)
	}

	// Idempotent re-insert of first trace should NOT change latest
	if err := repo.InsertTrace(ctx, &DecisionTrace{
		DecisionHash: "0xfirst",
		AgentID:      "agent-seq",
		PrevHash:     "0x0",
		TraceJSON:    []byte(`{"seq":1}`),
	}); err != nil {
		t.Fatal(err)
	}

	hash, _ = repo.GetLatestTraceHash(ctx, "agent-seq")
	if hash != "0xsecond" {
		t.Errorf("latest = %s, want 0xsecond (idempotent re-insert should not change latest)", hash)
	}
}

// TestRiskQueryMethods tests GetDayStartValue and GetRollingPeak24h.
func TestRiskQueryMethods(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()

	if _, err := repo.GetOrCreateAgent(ctx, "risk-agent", decimal.NewFromInt(10000)); err != nil {
		t.Fatal(err)
	}

	t.Run("GetDayStartValue", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func()
			want  decimal.Decimal
		}{
			{
				name:  "no trades returns zero",
				setup: func() {},
				want:  decimal.Zero,
			},
			{
				name: "yesterday's last trade",
				setup: func() {
					yesterday := time.Now().UTC().Add(-25 * time.Hour)
					_ = repo.RecordTrade(ctx, &StateUpdate{
						AgentID: "risk-agent", CashDelta: decimal.NewFromInt(-1000),
						PeakValue: decimal.NewFromInt(10000), FillCountIncr: 1,
					}, &TradeRecord{
						AgentID: "risk-agent", Timestamp: yesterday,
						Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromInt(1),
						Price: decimal.NewFromInt(1000), Value: decimal.NewFromInt(1000),
						Status: "fill", ValueAfter: decimal.NewFromInt(9800),
					})
				},
				want: decimal.NewFromInt(9800),
			},
			{
				name: "today's trade does not affect day start",
				setup: func() {
					_ = repo.RecordTrade(ctx, &StateUpdate{
						AgentID: "risk-agent", CashDelta: decimal.NewFromInt(500),
						PeakValue: decimal.NewFromInt(10000), FillCountIncr: 1,
					}, &TradeRecord{
						AgentID: "risk-agent", Timestamp: time.Now().UTC(),
						Pair: "ETH-USDC", Side: "sell", Qty: decimal.NewFromFloat(0.5),
						Price: decimal.NewFromInt(1100), Value: decimal.NewFromInt(500),
						PnL: decimal.NewFromInt(50), Status: "fill", ValueAfter: decimal.NewFromInt(10300),
					})
				},
				want: decimal.NewFromInt(9800), // still yesterday's value
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tt.setup()
				got, err := repo.GetDayStartValue(ctx, "risk-agent")
				if err != nil {
					t.Fatal(err)
				}
				if !got.Equal(tt.want) {
					t.Errorf("GetDayStartValue = %s, want %s", got, tt.want)
				}
			})
		}
	})

	t.Run("GetRollingPeak24h", func(t *testing.T) {
		repo3 := NewMemRepository()
		if _, err := repo3.GetOrCreateAgent(ctx, "peak-agent", decimal.NewFromInt(10000)); err != nil {
			t.Fatal(err)
		}

		// No trades - returns zero
		got, err := repo3.GetRollingPeak24h(ctx, "peak-agent")
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Errorf("GetRollingPeak24h (no trades) = %s, want 0", got)
		}

		// Add trades at different times
		_ = repo3.RecordTrade(ctx, &StateUpdate{
			AgentID: "peak-agent", CashDelta: decimal.NewFromInt(-1000),
			PeakValue: decimal.NewFromInt(10000), FillCountIncr: 1,
		}, &TradeRecord{
			AgentID: "peak-agent", Timestamp: time.Now().UTC().Add(-2 * time.Hour),
			Pair: "ETH-USDC", Side: "buy", Qty: decimal.NewFromInt(1),
			Price: decimal.NewFromInt(1000), Value: decimal.NewFromInt(1000),
			Status: "fill", ValueAfter: decimal.NewFromInt(10500),
		})
		_ = repo3.RecordTrade(ctx, &StateUpdate{
			AgentID: "peak-agent", CashDelta: decimal.NewFromInt(-500),
			PeakValue: decimal.NewFromInt(10500), FillCountIncr: 1,
		}, &TradeRecord{
			AgentID: "peak-agent", Timestamp: time.Now().UTC().Add(-1 * time.Hour),
			Pair: "SOL-USDC", Side: "buy", Qty: decimal.NewFromInt(5),
			Price: decimal.NewFromInt(100), Value: decimal.NewFromInt(500),
			Status: "fill", ValueAfter: decimal.NewFromInt(10200),
		})

		got, err = repo3.GetRollingPeak24h(ctx, "peak-agent")
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(decimal.NewFromInt(10500)) {
			t.Errorf("GetRollingPeak24h = %s, want 10500 (highest in 24h)", got)
		}
	})
}

// TestFailedAlertDeliveryOnceAfterAck verifies failed alerts are surfaced exactly once,
// including the race where an alert is acked before auto-execute later marks it failed.
func TestFailedAlertDeliveryOnceAfterAck(t *testing.T) {
	repo := NewMemRepository()
	ctx := context.Background()
	agentID := "agent-alert-failed-once"
	alertID := "palert-failed-once"

	if err := repo.UpsertAlert(ctx, &AlertRecord{
		AlertID:     alertID,
		AgentID:     agentID,
		Service:     "trading",
		Status:      "active",
		OnTrigger:   "auto_execute",
		MaxTriggers: 1,
		Params: map[string]any{
			"pair": "ETH-USDC",
			"type": "stop_loss",
		},
	}); err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.MarkAlertTriggered(ctx, alertID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("expected alert to be claimed as triggered")
	}

	// Simulate agent polling + ack before auto-execute finishes.
	initial, err := repo.GetTriggeredAlerts(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) != 1 {
		t.Fatalf("initial triggered alerts = %d, want 1", len(initial))
	}
	if err := repo.AckTriggeredAlerts(ctx, []string{alertID}); err != nil {
		t.Fatal(err)
	}
	afterAck, err := repo.GetTriggeredAlerts(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAck) != 0 {
		t.Fatalf("triggered alerts after ack = %d, want 0", len(afterAck))
	}

	// Auto-execute later fails: alert must become visible again exactly once.
	if err := repo.FailAlert(ctx, alertID, "all retries exhausted"); err != nil {
		t.Fatal(err)
	}
	failed, err := repo.GetTriggeredAlerts(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 1 {
		t.Fatalf("triggered alerts after fail = %d, want 1", len(failed))
	}
	if failed[0].Status != "failed" {
		t.Fatalf("failed alert status = %q, want failed", failed[0].Status)
	}
	if got, _ := failed[0].Params["fail_reason"].(string); got != "all retries exhausted" {
		t.Fatalf("failed alert reason = %q, want %q", got, "all retries exhausted")
	}

	if err := repo.AckTriggeredAlerts(ctx, []string{alertID}); err != nil {
		t.Fatal(err)
	}
	final, err := repo.GetTriggeredAlerts(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(final) != 0 {
		t.Fatalf("triggered alerts after final ack = %d, want 0", len(final))
	}
}

// assertDecimalApprox checks that got is within tolerance of want.
func assertDecimalApprox(t *testing.T, name string, got, want, tolerance decimal.Decimal) {
	t.Helper()
	if got.Sub(want).Abs().GreaterThan(tolerance) {
		t.Errorf("%s = %s, want ~%s (tolerance %s)", name, got, want, tolerance)
	}
}
