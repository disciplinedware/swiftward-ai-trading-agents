package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"ai-trading-agents/internal/config"
	"ai-trading-agents/internal/db"
	"ai-trading-agents/internal/exchange"
	"ai-trading-agents/internal/platform"
	"ai-trading-agents/internal/swiftward"
)

func testServiceContext(t *testing.T) *platform.ServiceContext {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"test": {ID: "agent-test-001", Name: "Test Agent", APIKey: "key-test", InitialBalance: 10000},
		},
	}
	router := chi.NewRouter()
	return platform.NewServiceContext(ctx, zap.NewNop(), cfg, router, []string{config.RoleTradingMCP})
}

func testTradingService(t *testing.T) *Service {
	t.Helper()
	svcCtx := testServiceContext(t)
	exchClient := exchange.NewSimClient(zap.NewNop(), decimal.NewFromFloat(0.001))
	repo := db.NewMemRepository()
	return NewService(svcCtx, exchClient, repo)
}

// callMCP sends a JSON-RPC tool call to the test server and returns the parsed result.
func callMCP(t *testing.T, ts *httptest.Server, tool string, args map[string]any, agentID string) map[string]any {
	t.Helper()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	}
	data, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp/trading", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var rpcResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatal(err)
	}
	return rpcResp
}

// extractResult gets the parsed JSON from a successful tool call response.
func extractResult(t *testing.T, rpcResp map[string]any) map[string]any {
	t.Helper()
	result := rpcResp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// parseDecimal parses a string value from a JSON response as decimal.
func parseDecimal(t *testing.T, m map[string]any, key string) decimal.Decimal {
	t.Helper()
	s, ok := m[key].(string)
	if !ok {
		t.Fatalf("%s is not a string: %v (%T)", key, m[key], m[key])
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("parse %s=%q as decimal: %v", key, s, err)
	}
	return d
}

func TestHTTPRoundTrip(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	tests := []struct {
		name      string
		tool      string
		args      map[string]any
		agentID   string
		wantErr   bool
		checkFunc func(t *testing.T, m map[string]any)
	}{
		{
			name:    "submit_order with header",
			tool:    "trade/submit_order",
			args:    map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 100.0},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				if m["status"] != "fill" {
					t.Errorf("status = %v, want fill", m["status"])
				}
				fill := m["fill"].(map[string]any)
				if fill["pair"] != "ETH-USDC" {
					t.Errorf("fill.pair = %v, want ETH-USDC", fill["pair"])
				}
			},
		},
		{
			name:    "get_portfolio with header",
			tool:    "trade/get_portfolio",
			args:    map[string]any{},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				portfolio := m["portfolio"].(map[string]any)
				if _, ok := portfolio["cash"]; !ok {
					t.Error("missing portfolio.cash field")
				}
				if m["fill_count"].(float64) < 1 {
					t.Errorf("fill_count = %v, want >= 1", m["fill_count"])
				}
			},
		},
		{
			name:    "get_history after trade",
			tool:    "trade/get_history",
			args:    map[string]any{"limit": 10.0},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				trades := m["trades"].([]any)
				if len(trades) < 1 {
					t.Error("expected at least 1 trade in history")
				}
			},
		},
		{
			name:    "get_limits",
			tool:    "trade/get_limits",
			args:    map[string]any{},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				portfolio := m["portfolio"].(map[string]any)
				if _, ok := portfolio["value"]; !ok {
					t.Error("missing portfolio.value field")
				}
			},
		},
		{
			name:    "get_portfolio_history",
			tool:    "trade/get_portfolio_history",
			args:    map[string]any{},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				if _, ok := m["equity_curve"]; !ok {
					t.Error("missing equity_curve field")
				}
			},
		},
		{
			name:    "estimate order",
			tool:    "trade/estimate_order",
			args:    map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 50.0},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				if _, ok := m["price"]; !ok {
					t.Error("missing price")
				}
				if _, ok := m["qty"]; !ok {
					t.Error("missing qty")
				}
			},
		},
		{
			name:    "heartbeat",
			tool:    "trade/heartbeat",
			args:    map[string]any{},
			agentID: agent,
			checkFunc: func(t *testing.T, m map[string]any) {
				portfolio := m["portfolio"].(map[string]any)
				if _, ok := portfolio["value"]; !ok {
					t.Error("missing portfolio.value")
				}
			},
		},
		{
			name:    "submit_order missing header",
			tool:    "trade/submit_order",
			args:    map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 100.0},
			agentID: "",
			wantErr: true,
		},
		{
			name:    "get_portfolio missing header",
			tool:    "trade/get_portfolio",
			args:    map[string]any{},
			agentID: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcResp := callMCP(t, ts, tt.tool, tt.args, tt.agentID)

			if tt.wantErr {
				result := rpcResp["result"].(map[string]any)
				if result["isError"] != true {
					t.Error("expected isError=true")
				}
				return
			}

			m := extractResult(t, rpcResp)
			if tt.checkFunc != nil {
				tt.checkFunc(t, m)
			}
		})
	}
}

func TestSubmitIntentHaltedAgent(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"
	if err := svc.SetHalted(context.Background(), agent, true); err != nil {
		t.Fatal(err)
	}

	rpcResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 50.0,
	}, agent)

	m := extractResult(t, rpcResp)
	if m["status"] != "reject" {
		t.Errorf("status = %v, want reject", m["status"])
	}
	reject := m["reject"].(map[string]any)
	if reject["verdict"] != "agent_halted" {
		t.Errorf("reject.verdict = %v, want agent_halted", reject["verdict"])
	}
}

// stubEvaluator is a test double for the policyEvaluator interface.
type stubEvaluator struct {
	result    *swiftward.EvalResult
	err       error
	lastEvent map[string]any // captures the last event data for assertions
}

func (s *stubEvaluator) EvaluateSync(_ context.Context, _, _, _ string, eventData map[string]any) (*swiftward.EvalResult, error) {
	s.lastEvent = eventData
	return s.result, s.err
}

func (s *stubEvaluator) EvaluateAsync(_ context.Context, _, _, _ string, _ map[string]any) (string, error) {
	return "stub-exec-id", nil
}

func testServiceWithEvaluator(t *testing.T, eval policyEvaluator) (*Service, *httptest.Server) {
	t.Helper()
	svc := testTradingService(t)
	svc.evaluator = eval
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	t.Cleanup(ts.Close)
	return svc, ts
}

func TestSubmitIntentEvidenceFields(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"
	rpcResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 50.0,
	}, agent)

	m := extractResult(t, rpcResp)

	if _, ok := m["decision_hash"]; !ok {
		t.Error("missing decision_hash in response")
	}
	if _, ok := m["prev_hash"]; !ok {
		t.Error("missing prev_hash in response")
	}
	// First submit: prev_hash must be ZeroHash
	if prev, _ := m["prev_hash"].(string); !strings.HasPrefix(prev, "0x") {
		t.Errorf("prev_hash %q is not 0x-prefixed", prev)
	}
}

func TestSubmitIntentHashChaining(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// First trade
	resp1 := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 10.0,
	}, agent))
	hash1, _ := resp1["decision_hash"].(string)

	if hash1 == "" {
		t.Fatal("first trade: missing decision_hash")
	}

	// Second trade: prev_hash must equal first trade's decision_hash
	resp2 := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "BTC-USDC", "side": "buy", "value": 20.0,
	}, agent))
	hash2, _ := resp2["decision_hash"].(string)
	prev2, _ := resp2["prev_hash"].(string)

	if hash2 == "" {
		t.Fatal("second trade: missing decision_hash")
	}
	if prev2 != hash1 {
		t.Errorf("second trade prev_hash = %q, want first trade hash %q", prev2, hash1)
	}
	if hash1 == hash2 {
		t.Error("first and second decision hashes must differ")
	}
}

func TestSubmitIntentHashChainingIndependentAgents(t *testing.T) {
	svc := testTradingService(t)
	// Register second agent
	svc.agents["agent-test-002"] = &config.AgentConfig{
		ID: "agent-test-002", Name: "Agent 2", APIKey: "key-2", InitialBalance: 5000,
	}
	if _, err := svc.repo.GetOrCreateAgent(context.Background(), "agent-test-002", decimal.NewFromInt(5000)); err != nil {
		t.Fatal(err)
	}

	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	r1 := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 10.0,
	}, "agent-test-001"))
	r2 := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 10.0,
	}, "agent-test-002"))

	// Both agents start from ZeroHash (independent chains)
	prev1, _ := r1["prev_hash"].(string)
	prev2, _ := r2["prev_hash"].(string)
	if prev1 != prev2 {
		t.Errorf("independent agents should both start from ZeroHash: agent1 prev=%q, agent2 prev=%q", prev1, prev2)
	}

	// But their decision_hashes should differ (different agent_id in trace data)
	h1, _ := r1["decision_hash"].(string)
	h2, _ := r2["decision_hash"].(string)
	if h1 == h2 {
		t.Error("different agents with same trade should produce different hashes (agent_id differs)")
	}
}

// TestHaltBlocksAndResumeAllows verifies the full halt/resume lifecycle:
// halt via DB -> submit_order rejected -> resume -> submit_order succeeds.
// This tests the code review fix: halt moved from process-local atomic.Bool to DB.
func TestHaltBlocksAndResumeAllows(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"
	ctx := context.Background()
	args := map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 50.0}

	// Trade should work before halt
	m := extractResult(t, callMCP(t, ts, "trade/submit_order", args, agent))
	if m["status"] != "fill" {
		t.Fatalf("expected fill before halt, got %v", m["status"])
	}

	// Halt agent
	if err := svc.SetHalted(ctx, agent, true); err != nil {
		t.Fatal(err)
	}

	// Verify halt is visible via GetAgent (DB-backed, not process-local)
	state, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Halted {
		t.Error("expected Halted=true after SetHalted")
	}

	// Trade should be rejected while halted
	m = extractResult(t, callMCP(t, ts, "trade/submit_order", args, agent))
	if m["status"] != "reject" {
		t.Errorf("expected reject, got %v", m["status"])
	}
	reject := m["reject"].(map[string]any)
	if reject["verdict"] != "agent_halted" {
		t.Errorf("expected reject.verdict=agent_halted, got %v", reject["verdict"])
	}

	// Resume agent
	if err := svc.SetHalted(ctx, agent, false); err != nil {
		t.Fatal(err)
	}

	// Trade should work again
	m = extractResult(t, callMCP(t, ts, "trade/submit_order", args, agent))
	if m["status"] != "fill" {
		t.Errorf("expected fill after resume, got %v", m["status"])
	}
}

func TestSubmitIntentSwiftwardApproved(t *testing.T) {
	eval := &stubEvaluator{result: &swiftward.EvalResult{
		ID: "eval-001", Verdict: swiftward.VerdictApproved,
	}}
	_, ts := testServiceWithEvaluator(t, eval)

	m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100.0,
	}, "agent-test-001"))

	if m["status"] != "fill" {
		t.Errorf("status = %v, want fill", m["status"])
	}
}

func TestSubmitIntentSwiftwardEnrichment(t *testing.T) {
	eval := &stubEvaluator{result: &swiftward.EvalResult{
		ID: "eval-001", Verdict: swiftward.VerdictApproved,
	}}
	_, ts := testServiceWithEvaluator(t, eval)

	// Make a trade first to have portfolio context
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100.0,
	}, "agent-test-001")

	// Second trade - check enrichment fields
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "BTC-USDC", "side": "buy", "value": 50.0,
	}, "agent-test-001")

	// Verify enrichment fields were sent to evaluator (nested structure)
	event := eval.lastEvent
	if event == nil {
		t.Fatal("evaluator was not called")
	}
	// Check order sub-object
	order, ok := event["order"].(map[string]any)
	if !ok {
		t.Fatal("missing 'order' sub-object in event data")
	}
	if order["pair"] != "BTC-USDC" {
		t.Errorf("order.pair = %v, want BTC-USDC", order["pair"])
	}
	// Check portfolio sub-object
	portfolio, ok := event["portfolio"].(map[string]any)
	if !ok {
		t.Fatal("missing 'portfolio' sub-object in event data")
	}
	if _, ok := portfolio["value"]; !ok {
		t.Error("missing portfolio.value in event data")
	}
	if _, ok := portfolio["cash"]; !ok {
		t.Error("missing portfolio.cash in event data")
	}
}

func TestSubmitIntentSwiftwardBlocked(t *testing.T) {
	tests := []struct {
		name       string
		eval       *stubEvaluator
		wantReason string
	}{
		{
			name: "rejected_verdict",
			eval: &stubEvaluator{result: &swiftward.EvalResult{
				ID:       "eval-001",
				Verdict:  swiftward.VerdictRejected,
				Response: map[string]any{"reason": "exceeds daily limit"},
			}},
			wantReason: "exceeds daily limit",
		},
		{
			name: "flagged_verdict",
			eval: &stubEvaluator{result: &swiftward.EvalResult{
				ID:      "eval-002",
				Verdict: swiftward.VerdictFlagged,
			}},
			wantReason: "Policy violation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ts := testServiceWithEvaluator(t, tt.eval)

			m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
				"pair": "ETH-USDC", "side": "buy", "value": 100.0,
			}, "agent-test-001"))

			if m["status"] != "reject" {
				t.Errorf("status = %v, want reject", m["status"])
			}
			reject := m["reject"].(map[string]any)
			if reject["reason"] != tt.wantReason {
				t.Errorf("reject.reason = %q, want %q", reject["reason"], tt.wantReason)
			}
		})
	}
}

// Swiftward is a gate: if the evaluator can't answer, trade is REJECTED
// (fail-closed). Prior behaviour was fail-open - a silent bug where an
// unreachable gate let every trade through. Use a terminal error (not a
// technical one) so the retry helper does not add 14s of backoff sleeps.
func TestSubmitIntentSwiftwardErrorFailClosed(t *testing.T) {
	eval := &stubEvaluator{err: fmt.Errorf("swiftward responded with invalid schema: field 'verdict' missing")}
	_, ts := testServiceWithEvaluator(t, eval)

	m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 50.0,
	}, "agent-test-001"))

	if m["status"] != "reject" {
		t.Fatalf("status = %v, want reject (fail-closed on evaluator error)", m["status"])
	}
	reject, ok := m["reject"].(map[string]any)
	if !ok {
		t.Fatalf("reject payload missing: %v", m)
	}
	if reject["source"] != RejectSourcePolicy {
		t.Errorf("reject.source = %v, want %q", reject["source"], RejectSourcePolicy)
	}
	reason, _ := reject["reason"].(string)
	if !strings.HasPrefix(reason, "swiftward_unavailable:") {
		t.Errorf("reject.reason = %q, want prefix %q", reason, "swiftward_unavailable:")
	}
}

func TestToolsNoAgentIDInSchema(t *testing.T) {
	svc := testTradingService(t)
	tools := svc.tools()

	for _, tool := range tools {
		props, ok := tool.InputSchema["properties"].(map[string]any)
		if !ok {
			continue
		}
		if _, hasAgentID := props["agent_id"]; hasAgentID {
			t.Errorf("tool %s should not have agent_id in schema", tool.Name)
		}
	}
}

func TestSubmitIntentPersistsToRepository(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Buy
	m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100.0,
	}, agent))
	if m["status"] != "fill" {
		t.Fatal("expected fill")
	}

	// Verify trade is in DB
	ctx := context.Background()
	trades, err := svc.repo.GetTradeHistory(ctx, agent, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].Pair != "ETH-USDC" || trades[0].Side != "buy" {
		t.Errorf("trade mismatch: %s %s", trades[0].Pair, trades[0].Side)
	}

	// Verify agent state updated
	state, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if state.FillCount != 1 {
		t.Errorf("fill_count = %d, want 1", state.FillCount)
	}
	if state.Cash.GreaterThanOrEqual(decimal.NewFromInt(10000)) {
		t.Errorf("cash = %s, should be less than initial 10000 after buy", state.Cash)
	}

	// Verify open position
	positions, err := svc.repo.GetOpenPositions(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].Pair != "ETH-USDC" {
		t.Errorf("position pair = %s, want ETH-USDC", positions[0].Pair)
	}
}

func TestSellPnLAndPositionClosure(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"
	ctx := context.Background()

	// Buy $200 of ETH
	m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 200.0,
	}, agent))
	if m["status"] != "fill" {
		t.Fatal("buy not filled")
	}
	fill := m["fill"].(map[string]any)
	buyPrice := parseDecimal(t, fill, "price")
	buyQty := parseDecimal(t, fill, "qty")

	stateAfterBuy, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	cashAfterBuy := stateAfterBuy.Cash

	// Sell same amount of ETH (in USD terms)
	sellValue := buyQty.Mul(buyPrice).InexactFloat64() // sell all at current price
	m2 := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "sell", "value": sellValue,
	}, agent))
	if m2["status"] != "fill" {
		t.Fatal("sell not filled")
	}

	// Cash should increase after sell
	stateAfterSell, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if stateAfterSell.Cash.LessThanOrEqual(cashAfterBuy) {
		t.Errorf("cash after sell (%s) should be > cash after buy (%s)", stateAfterSell.Cash, cashAfterBuy)
	}
	if stateAfterSell.FillCount != 2 {
		t.Errorf("fill_count = %d, want 2", stateAfterSell.FillCount)
	}

	// Position should be closed (or near zero)
	positions, err := svc.repo.GetOpenPositions(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	for _, pos := range positions {
		if pos.Pair == "ETH-USDC" {
			t.Errorf("ETH-USDC position should be closed, got qty=%s", pos.Qty)
		}
	}

	// Verify trade records
	trades, err := svc.repo.GetTradeHistory(ctx, agent, 10, "ETH-USDC", "fill")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 2 {
		t.Fatalf("expected 2 ETH-USDC trades, got %d", len(trades))
	}
	// Trades are DESC - first is the sell
	sellTrade := trades[0]
	if sellTrade.Side != "sell" {
		t.Errorf("expected sell trade first (DESC), got %s", sellTrade.Side)
	}
}

func TestSubmitIntentSwiftwardEnrichmentValues(t *testing.T) {
	eval := &stubEvaluator{result: &swiftward.EvalResult{
		ID: "eval-001", Verdict: swiftward.VerdictApproved,
	}}
	_, ts := testServiceWithEvaluator(t, eval)

	agent := "agent-test-001"

	// First trade: buy $500 of ETH
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 500.0,
	}, agent)

	// Second trade - enrichment should reflect the state after first trade
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "BTC-USDC", "side": "buy", "value": 100.0,
	}, agent)

	event := eval.lastEvent
	if event == nil {
		t.Fatal("evaluator was not called")
	}

	// Event data is now nested: order + portfolio sub-objects
	portfolio, ok := event["portfolio"].(map[string]any)
	if !ok {
		t.Fatal("missing 'portfolio' sub-object in event data")
	}

	// Portfolio value should be around initial 10000
	pv, ok := portfolio["value"].(float64)
	if !ok || pv < 9000 || pv > 11000 {
		t.Errorf("portfolio.value = %v, want ~10000", portfolio["value"])
	}

	// Cash should be reduced by ~500 after first buy
	cash, ok := portfolio["cash"].(float64)
	if !ok || cash > 9600 || cash < 9400 {
		t.Errorf("portfolio.cash = %v, want ~9500 (10000 - 500)", portfolio["cash"])
	}
}

func TestHeartbeatPeakValueUpdate(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"
	ctx := context.Background()

	// Buy some ETH to create a position
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100.0,
	}, agent)

	// Get state before heartbeat
	stateBefore, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}

	// Call heartbeat
	m := extractResult(t, callMCP(t, ts, "trade/heartbeat", map[string]any{}, agent))

	portfolio := m["portfolio"].(map[string]any)
	peakReturned := parseDecimal(t, portfolio, "peak")
	if peakReturned.LessThan(stateBefore.PeakValue) {
		t.Errorf("portfolio.peak from heartbeat (%s) < state peak (%s)", peakReturned, stateBefore.PeakValue)
	}

	// Verify peak was persisted
	stateAfter, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if stateAfter.PeakValue.LessThan(stateBefore.PeakValue) {
		t.Errorf("persisted peak (%s) < previous peak (%s)", stateAfter.PeakValue, stateBefore.PeakValue)
	}
}

func TestEstimate(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	tests := []struct {
		name        string
		args        map[string]any
		wantWarning string
		checkFunc   func(t *testing.T, m map[string]any)
	}{
		{
			name:        "insufficient cash",
			args:        map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 50000.0},
			wantWarning: "insufficient cash",
		},
		{
			name: "valid estimate",
			args: map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 100.0},
			checkFunc: func(t *testing.T, m map[string]any) {
				price := parseDecimal(t, m, "price")
				if !price.IsPositive() {
					t.Error("expected positive price")
				}
				qty := parseDecimal(t, m, "qty")
				if !qty.IsPositive() {
					t.Error("expected positive qty")
				}
				portfolio := m["portfolio"].(map[string]any)
				cashAvail := parseDecimal(t, portfolio, "cash")
				if !cashAvail.Equal(decimal.NewFromInt(10000)) {
					t.Errorf("portfolio.cash = %s, want 10000", cashAvail)
				}
				if _, ok := m["position_pct_after"]; !ok {
					t.Error("missing position_pct_after")
				}
			},
		},
		{
			name: "sell estimate no warning",
			args: map[string]any{"pair": "ETH-USDC", "side": "sell", "value": 50000.0},
			checkFunc: func(t *testing.T, m map[string]any) {
				if _, ok := m["warning"]; ok {
					t.Error("sell should not warn about insufficient cash")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := extractResult(t, callMCP(t, ts, "trade/estimate_order", tt.args, agent))
			if tt.wantWarning != "" {
				if m["warning"] != tt.wantWarning {
					t.Errorf("warning = %v, want %q", m["warning"], tt.wantWarning)
				}
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, m)
			}
		})
	}
}

func TestEstimateInvalidSide(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	rpcResp := callMCP(t, ts, "trade/estimate_order", map[string]any{
		"pair": "ETH-USDC", "side": "short", "value": 100.0,
	}, "agent-test-001")
	result := rpcResp["result"].(map[string]any)
	if result["isError"] != true {
		t.Error("expected isError=true for invalid side")
	}
}

func TestSubmitIntentValidation(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	tests := []struct {
		name    string
		tool    string
		args    map[string]any
		agentID string
	}{
		{"missing pair", "trade/submit_order", map[string]any{"side": "buy", "value": 100.0}, "agent-test-001"},
		{"missing side", "trade/submit_order", map[string]any{"pair": "ETH-USDC", "value": 100.0}, "agent-test-001"},
		{"invalid side", "trade/submit_order", map[string]any{"pair": "ETH-USDC", "side": "short", "value": 100.0}, "agent-test-001"},
		{"zero value", "trade/submit_order", map[string]any{"pair": "ETH-USDC", "side": "buy", "value": 0.0}, "agent-test-001"},
		{"negative value", "trade/submit_order", map[string]any{"pair": "ETH-USDC", "side": "buy", "value": -10.0}, "agent-test-001"},
		// "unknown agent" case removed: dynamic registration auto-creates agents on first submit_order
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcResp := callMCP(t, ts, tt.tool, tt.args, tt.agentID)
			result := rpcResp["result"].(map[string]any)
			if result["isError"] != true {
				t.Errorf("expected isError=true for %s", tt.name)
			}
		})
	}
}

func TestDecisionHashPersistedInTradeRecord(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"
	ctx := context.Background()

	m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100.0,
	}, agent))

	responseHash, _ := m["decision_hash"].(string)
	if responseHash == "" {
		t.Fatal("expected decision_hash in response")
	}

	// Verify it was persisted to DB
	trades, err := svc.repo.GetTradeHistory(ctx, agent, 1, "", "fill")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].DecisionHash != responseHash {
		t.Errorf("DB decision_hash = %q, want %q", trades[0].DecisionHash, responseHash)
	}
}

func TestRejectedTradePersistedInHistory(t *testing.T) {
	eval := &stubEvaluator{result: &swiftward.EvalResult{
		ID:       "eval-001",
		Verdict:  swiftward.VerdictRejected,
		Response: map[string]any{"reason": "too risky"},
	}}
	svc, ts := testServiceWithEvaluator(t, eval)

	agent := "agent-test-001"

	// Submit a trade that will be rejected by policy
	m := extractResult(t, callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100.0,
	}, agent))
	if m["status"] != "reject" {
		t.Fatal("expected reject")
	}

	// Verify rejected trade is in DB
	ctx := context.Background()
	trades, err := svc.repo.GetTradeHistory(ctx, agent, 10, "", "reject")
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 rejected trade, got %d", len(trades))
	}
	if trades[0].Status != "reject" {
		t.Errorf("status = %s, want reject", trades[0].Status)
	}

	// Verify rejected count incremented
	state, err := svc.repo.GetAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	if state.RejectCount != 1 {
		t.Errorf("reject_count = %d, want 1", state.RejectCount)
	}
}

func TestSubmitIntentInsufficientBalance(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	tests := []struct {
		name       string
		side       string
		value      float64
		wantStatus string
	}{
		{"buy exceeding cash rejected", "buy", 999999, "reject"},
		{"buy within cash fills", "buy", 100, "fill"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpcResp := callMCP(t, ts, "trade/submit_order", map[string]any{
				"pair": "ETH-USDC", "side": tt.side, "value": tt.value,
			}, agent)

			m := extractResult(t, rpcResp)
			if m["status"] != tt.wantStatus {
				t.Errorf("status = %v, want %s", m["status"], tt.wantStatus)
			}
			if tt.wantStatus == "reject" {
				reject := m["reject"].(map[string]any)
				if reject["source"] != "policy" {
					t.Errorf("reject.source = %v, want policy", reject["source"])
				}
				if reason, ok := reject["reason"].(string); !ok || reason == "" {
					t.Error("expected non-empty reject.reason")
				}
			}
		})
	}
}

func TestSellExceedingPositionRejected(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	t.Cleanup(ts.Close)

	agent := "agent-test-001"

	// Buy 100 USDC worth of ETH first to create a position.
	buyResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 100,
	}, agent)
	buyResult := extractResult(t, buyResp)
	if buyResult["status"] != "fill" {
		t.Fatalf("setup buy failed: status = %v", buyResult["status"])
	}

	tests := []struct {
		name       string
		sellValue  float64
		wantStatus string
	}{
		{"sell within position fills", 50, "fill"},
		{"sell exceeding position clamped to all", 99999, "fill"},
		{"sell with no position rejected (different pair)", 10, "reject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pair := "ETH-USDC"
			if tt.name == "sell with no position rejected (different pair)" {
				pair = "BTC-USDC"
			}
			rpcResp := callMCP(t, ts, "trade/submit_order", map[string]any{
				"pair": pair, "side": "sell", "value": tt.sellValue,
			}, agent)
			m := extractResult(t, rpcResp)
			if m["status"] != tt.wantStatus {
				t.Errorf("status = %v, want %s", m["status"], tt.wantStatus)
			}
		})
	}
}

func TestCommissionTracking(t *testing.T) {
	svc := testTradingService(t)
	_ = svc.Initialize()
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()
	agent := "agent-test-001"

	// Buy $1000 of ETH-USDC with 0.1% commission.
	buyResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 1000,
	}, agent)
	buyResult := extractResult(t, buyResp)
	if buyResult["status"] != StatusFill {
		t.Fatalf("buy status = %v, want fill", buyResult["status"])
	}

	fill, _ := buyResult["fill"].(map[string]any)

	// Fee should be present and positive.
	feeStr, _ := fill["fee"].(string)
	fee, _ := decimal.NewFromString(feeStr)
	if !fee.IsPositive() {
		t.Errorf("buy fee = %s, want positive", feeStr)
	}

	// Buy fee should be in base asset (ETH).
	if fill["fee_asset"] != "ETH" {
		t.Errorf("buy fee_asset = %v, want ETH", fill["fee_asset"])
	}

	// fee_value should be fee * price (converted to portfolio currency).
	feeValueStr, _ := fill["fee_value"].(string)
	feeValue, _ := decimal.NewFromString(feeValueStr)
	if !feeValue.IsPositive() {
		t.Errorf("buy fee_value = %s, want positive", feeValueStr)
	}

	// Qty should be reduced by fee (net qty < gross qty).
	qtyStr, _ := fill["qty"].(string)
	qty, _ := decimal.NewFromString(qtyStr)
	priceStr, _ := fill["price"].(string)
	price, _ := decimal.NewFromString(priceStr)
	grossQty := decimal.NewFromInt(1000).Div(price).Round(6)
	if qty.GreaterThanOrEqual(grossQty) {
		t.Errorf("buy qty %s should be less than gross qty %s", qty, grossQty)
	}

	// Value should be the full cash paid (1000).
	valueStr, _ := fill["value"].(string)
	value, _ := decimal.NewFromString(valueStr)
	if !value.Equal(decimal.NewFromInt(1000)) {
		t.Errorf("buy value = %s, want 1000", valueStr)
	}

	// Portfolio should show total_fees.
	portResp := callMCP(t, ts, "trade/get_portfolio", nil, agent)
	portResult := extractResult(t, portResp)
	totalFeesStr, _ := portResult["total_fees"].(string)
	totalFees, _ := decimal.NewFromString(totalFeesStr)
	if !totalFees.IsPositive() {
		t.Errorf("total_fees after buy = %s, want positive", totalFeesStr)
	}

	// Now sell - use small value to stay within position.
	sellResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "sell", "value": 200,
	}, agent)
	sellResult := extractResult(t, sellResp)
	if sellResult["status"] != StatusFill {
		t.Fatalf("sell status = %v, want fill", sellResult["status"])
	}

	sellFill, _ := sellResult["fill"].(map[string]any)

	// Sell fee should be in quote asset (USDC).
	if sellFill["fee_asset"] != "USDC" {
		t.Errorf("sell fee_asset = %v, want USDC", sellFill["fee_asset"])
	}

	sellFeeStr, _ := sellFill["fee"].(string)
	sellFee, _ := decimal.NewFromString(sellFeeStr)
	if !sellFee.IsPositive() {
		t.Errorf("sell fee = %s, want positive", sellFeeStr)
	}

	// Total fees should increase after sell.
	portResp2 := callMCP(t, ts, "trade/get_portfolio", nil, agent)
	portResult2 := extractResult(t, portResp2)
	totalFees2Str, _ := portResult2["total_fees"].(string)
	totalFees2, _ := decimal.NewFromString(totalFees2Str)
	if totalFees2.LessThanOrEqual(totalFees) {
		t.Errorf("total_fees after sell (%s) should be > after buy (%s)", totalFees2, totalFees)
	}
}

func TestIsRiskReducing(t *testing.T) {
	tests := []struct {
		name      string
		side      string
		pair      string
		positions []db.OpenPosition
		want      bool
	}{
		{
			name: "sell held position is risk-reducing",
			side: "sell", pair: "ETH-USDC",
			positions: []db.OpenPosition{{Pair: "ETH-USDC", Qty: decimal.NewFromInt(1)}},
			want:      true,
		},
		{
			name: "buy with existing position is risk-increasing",
			side: "buy", pair: "ETH-USDC",
			positions: []db.OpenPosition{{Pair: "ETH-USDC", Qty: decimal.NewFromInt(1)}},
			want:      false,
		},
		{
			name: "buy new pair is risk-increasing",
			side: "buy", pair: "SOL-USDC",
			positions: []db.OpenPosition{{Pair: "ETH-USDC", Qty: decimal.NewFromInt(1)}},
			want:      false,
		},
		{
			name:      "sell with no positions is risk-increasing",
			side:      "sell", pair: "ETH-USDC",
			positions: nil,
			want:      false,
		},
		{
			name: "sell different pair is risk-increasing",
			side: "sell", pair: "SOL-USDC",
			positions: []db.OpenPosition{{Pair: "ETH-USDC", Qty: decimal.NewFromInt(1)}},
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRiskReducing(tt.side, tt.pair, tt.positions)
			if got != tt.want {
				t.Errorf("isRiskReducing(%s, %s) = %v, want %v", tt.side, tt.pair, got, tt.want)
			}
		})
	}
}

func TestComputeRiskTier(t *testing.T) {
	tests := []struct {
		name     string
		drawdown float64
		want     int
	}{
		{"no drawdown", 0.0, 0},
		{"small gain", 0.01, 0},
		{"just above tier 1", -0.019, 0},
		{"at tier 1 boundary", -0.02, 1},    // exactly -2% = tier 1 (at-or-below, industry convention)
		{"tier 1", -0.025, 1},
		{"just above tier 2", -0.034, 1},
		{"at tier 2 boundary", -0.035, 2},   // exactly -3.5% = tier 2
		{"tier 2", -0.04, 2},
		{"just above tier 3", -0.049, 2},
		{"at tier 3 boundary", -0.05, 3},    // exactly -5% = tier 3 (close-only)
		{"tier 3 (close-only)", -0.06, 3},
		{"deep drawdown", -0.15, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeRiskTier(tt.drawdown)
			if got != tt.want {
				t.Errorf("computeRiskTier(%f) = %d, want %d", tt.drawdown, got, tt.want)
			}
		})
	}
}

// ── Conditional order (alert) tests ────────────────────────────────────────

func TestSetConditional_CreatesTier2(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	resp := callMCP(t, ts, "trade/set_conditional", map[string]any{
		"pair":          "ETH-USDC",
		"type":          "stop_loss",
		"trigger_price": 3000.0,
	}, agent)
	m := extractResult(t, resp)

	if m["status"] != "active" {
		t.Errorf("status = %v, want active", m["status"])
	}
	if m["tier"] != "2" {
		t.Errorf("tier = %v, want 2 (native stops disabled by default)", m["tier"])
	}
	alertID, _ := m["alert_id"].(string)
	if alertID == "" {
		t.Fatal("missing alert_id in response")
	}

	// Verify it shows up in alert/list
	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	alerts, _ := listM["alerts"].([]any)
	if len(alerts) == 0 {
		t.Fatal("alert/list returned no alerts after set_conditional")
	}
	first := alerts[0].(map[string]any)
	if first["alert_id"] != alertID {
		t.Errorf("alert/list alert_id = %v, want %v", first["alert_id"], alertID)
	}
}

func TestSetConditional_InformAgentDefault(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Default: inform_agent should be true
	resp := callMCP(t, ts, "trade/set_conditional", map[string]any{
		"pair": "BTC-USDC", "type": "take_profit", "trigger_price": 100000.0,
	}, agent)
	m := extractResult(t, resp)
	alertID := m["alert_id"].(string)

	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	for _, a := range listM["alerts"].([]any) {
		rec := a.(map[string]any)
		if rec["alert_id"] == alertID {
			if rec["inform_agent"] != true {
				t.Errorf("inform_agent = %v, want true (default)", rec["inform_agent"])
			}
			return
		}
	}
	t.Error("alert not found in alert/list")
}

func TestSetConditional_InformAgentFalse(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	resp := callMCP(t, ts, "trade/set_conditional", map[string]any{
		"pair": "ETH-USDC", "type": "stop_loss", "trigger_price": 3000.0,
		"inform_agent": false,
	}, agent)
	m := extractResult(t, resp)
	alertID := m["alert_id"].(string)

	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	for _, a := range listM["alerts"].([]any) {
		rec := a.(map[string]any)
		if rec["alert_id"] == alertID {
			if rec["inform_agent"] != false {
				t.Errorf("inform_agent = %v, want false", rec["inform_agent"])
			}
			return
		}
	}
	t.Error("alert not found in alert/list")
}

func TestCancelConditional(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Create a conditional order
	resp := callMCP(t, ts, "trade/set_conditional", map[string]any{
		"pair": "ETH-USDC", "type": "stop_loss", "trigger_price": 3000.0,
	}, agent)
	m := extractResult(t, resp)
	alertID := m["alert_id"].(string)

	// Cancel it
	cancelResp := callMCP(t, ts, "trade/cancel_conditional", map[string]any{
		"alert_id": alertID,
	}, agent)
	cancelM := extractResult(t, cancelResp)
	if cancelM["cancelled"] != true {
		t.Errorf("cancelled = %v, want true", cancelM["cancelled"])
	}

	// Verify it no longer shows up in alert/list
	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	count, _ := listM["count"].(float64)
	if count != 0 {
		t.Errorf("alert/list count = %v after cancel, want 0", count)
	}
}

func TestAlertList_AggregatesServices(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Create a trading conditional
	callMCP(t, ts, "trade/set_conditional", map[string]any{
		"pair": "ETH-USDC", "type": "stop_loss", "trigger_price": 3000.0,
	}, agent)

	// Create a reminder (time service)
	callMCP(t, ts, "trade/set_reminder", map[string]any{
		"fire_at": "2099-01-01T00:00:00Z",
		"note":    "check positions",
	}, agent)

	// alert/list should return both
	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	count, _ := listM["count"].(float64)
	if count < 2 {
		t.Errorf("alert/list count = %v, want >= 2 (trading + time)", count)
	}

	// Verify services are different
	services := make(map[string]bool)
	for _, a := range listM["alerts"].([]any) {
		rec := a.(map[string]any)
		if svc, ok := rec["service"].(string); ok {
			services[svc] = true
		}
	}
	if !services["trading"] {
		t.Error("expected trading service in alert/list")
	}
	if !services["time"] {
		t.Error("expected time service in alert/list")
	}
}

func TestSubmitOrder_AutoCreatesSLTP(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Buy with SL and TP in params
	buyResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair":  "ETH-USDC",
		"side":  "buy",
		"value": 500.0,
		"params": map[string]any{
			"stop_loss":   3000.0,
			"take_profit": 4000.0,
			"reasoning":   "test",
		},
	}, agent)
	buyM := extractResult(t, buyResp)
	if buyM["status"] != "fill" {
		t.Fatalf("buy status = %v, want fill", buyM["status"])
	}

	// alert/list should have 2 conditional orders (SL + TP) in same OCO group
	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	count, _ := listM["count"].(float64)
	if count < 2 {
		t.Errorf("alert/list count = %v after buy with SL+TP, want >= 2", count)
	}

	// Check that we have both types
	types := make(map[string]bool)
	for _, a := range listM["alerts"].([]any) {
		rec := a.(map[string]any)
		params, _ := rec["params"].(map[string]any)
		if params != nil {
			if tp, ok := params["type"].(string); ok {
				types[tp] = true
			}
		}
	}
	if !types["stop_loss"] {
		t.Error("expected stop_loss conditional order")
	}
	if !types["take_profit"] {
		t.Error("expected take_profit conditional order")
	}
}

func TestSubmitOrder_SellCancelsConditionals(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Buy with SL
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 500.0,
		"params": map[string]any{"stop_loss": 3000.0},
	}, agent)

	// Sell the position
	sellResp := callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "sell", "value": 500.0,
	}, agent)
	sellResult := extractResult(t, sellResp)
	sellStatus, _ := sellResult["status"].(string)
	if sellStatus != "fill" {
		t.Fatalf("sell status = %q, want fill; result = %v", sellStatus, sellResult)
	}

	// alert/list should be empty (sell cancels conditionals for that pair)
	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	count, _ := listM["count"].(float64)
	if count != 0 {
		t.Errorf("alert/list count = %v after sell, want 0 (conditionals should be cancelled)", count)
	}
}

func TestSubmitOrder_InformAgentThreadedFromParams(t *testing.T) {
	svc := testTradingService(t)
	if err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(svc.svcCtx.Router())
	defer ts.Close()

	agent := "agent-test-001"

	// Buy with SL and inform_agent=false
	callMCP(t, ts, "trade/submit_order", map[string]any{
		"pair": "ETH-USDC", "side": "buy", "value": 500.0,
		"params": map[string]any{
			"stop_loss":    3000.0,
			"inform_agent": false,
		},
	}, agent)

	listResp := callMCP(t, ts, "alert/list", map[string]any{}, agent)
	listM := extractResult(t, listResp)
	alerts, _ := listM["alerts"].([]any)
	if len(alerts) == 0 {
		t.Fatal("expected at least 1 alert")
	}
	for _, a := range alerts {
		rec := a.(map[string]any)
		if rec["inform_agent"] != false {
			t.Errorf("inform_agent = %v, want false (threaded from submit_order params)", rec["inform_agent"])
		}
	}
}
