package exchange

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

func TestToKrakenPair(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"BTC-USD", "XBTUSD"},   // BTC -> XBT
		{"BTC-USDT", "XBTUSDT"}, // no cross-quote
		{"BTC-USDC", "XBTUSDC"}, // no cross-quote
		{"ETH-USD", "ETHUSD"},
		{"ETH-USDT", "ETHUSDT"},
		{"SOL-USD", "SOLUSD"},
		{"DOGE-USD", "XDGUSD"}, // DOGE -> XDG
		{"DOGE-USDT", "XDGUSDT"},
		{"ETH-BTC", "ETHXBT"}, // non-stablecoin quote
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toKrakenPair(tt.input)
			if got != tt.want {
				t.Errorf("toKrakenPair(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFromKrakenPair(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"XBTUSD", "BTC-USD"},
		{"XBTUSDT", "BTC-USDT"},
		{"ETHUSD", "ETH-USD"},
		{"ETHUSDT", "ETH-USDT"},
		{"SOLUSD", "SOL-USD"},
		{"XDGUSD", "DOGE-USD"},
		{"XDGUSDT", "DOGE-USDT"},
		{"LINKUSD", "LINK-USD"},
		{"SUIUSD", "SUI-USD"},
		{"ETHXBT", "ETH-BTC"}, // non-stablecoin quote (XBT->BTC first, then BTC is quote)
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := fromKrakenPair(tt.input)
			if got != tt.want {
				t.Errorf("fromKrakenPair(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFill_Buy(t *testing.T) {
	raw := []byte(`{"action":"market_order_filled","cost":6.673680000000001,"fee":0.017351568,"mode":"paper","order_id":"PAPER-00001","pair":"XBTUSD","price":66736.8,"side":"buy","trade_id":"PAPER-00002","volume":0.0001}`)

	c := &KrakenClient{log: zap.NewNop(), lastPrices: make(map[string]decimal.Decimal)}
	req := &TradeRequest{Pair: "BTC-USD", Side: "buy"}
	resp, err := c.parseFill(raw, req)
	if err != nil {
		t.Fatalf("parseFill: %v", err)
	}

	if resp.Status != StatusFilled {
		t.Errorf("status = %q, want %q", resp.Status, StatusFilled)
	}
	if resp.FillID != "PAPER-00002" {
		t.Errorf("fillID = %q, want PAPER-00002", resp.FillID)
	}
	if resp.Price.InexactFloat64() != 66736.8 {
		t.Errorf("price = %s, want 66736.8", resp.Price.String())
	}
	if resp.Pair != "BTC-USD" {
		t.Errorf("pair = %q, want BTC-USD", resp.Pair)
	}
	// Buy: QuoteQty = cost (full cash paid)
	cost := decimal.NewFromFloat(6.673680000000001)
	if !resp.QuoteQty.Equal(cost) {
		t.Errorf("quoteQty = %s, want %s (cost)", resp.QuoteQty.String(), cost.String())
	}
	// Buy: Fee must be in BASE (tiny BTC), not QUOTE (dollars)
	if resp.Fee.IsZero() {
		t.Error("fee should not be zero")
	}
	if resp.Fee.GreaterThanOrEqual(decimal.NewFromFloat(0.001)) {
		t.Errorf("buy fee = %s looks like quote units, expected base units (< 0.001 BTC)", resp.Fee.String())
	}
	// Buy: Qty should be net (less than gross volume)
	grossQty := decimal.NewFromFloat(0.0001)
	if resp.Qty.GreaterThanOrEqual(grossQty) {
		t.Errorf("buy qty = %s should be less than gross %s (net of fee)", resp.Qty.String(), grossQty.String())
	}
}

func TestParseFill_Sell(t *testing.T) {
	raw := []byte(`{"action":"market_order_filled","cost":6.67367,"fee":0.017351542,"mode":"paper","order_id":"PAPER-00003","pair":"XBTUSD","price":66736.7,"side":"sell","trade_id":"PAPER-00004","volume":0.0001}`)

	c := &KrakenClient{log: zap.NewNop(), lastPrices: make(map[string]decimal.Decimal)}
	req := &TradeRequest{Pair: "BTC-USD", Side: "sell"}
	resp, err := c.parseFill(raw, req)
	if err != nil {
		t.Fatalf("parseFill: %v", err)
	}

	if resp.Status != StatusFilled {
		t.Errorf("status = %q, want %q", resp.Status, StatusFilled)
	}
	// Sell: Qty = full volume
	grossQty := decimal.NewFromFloat(0.0001)
	if !resp.Qty.Equal(grossQty) {
		t.Errorf("sell qty = %s, want %s (full volume)", resp.Qty.String(), grossQty.String())
	}
	// Sell: QuoteQty = cost - fee (net cash)
	cost := decimal.NewFromFloat(6.67367)
	fee := decimal.NewFromFloat(0.017351542)
	expectedQuote := cost.Sub(fee)
	if !resp.QuoteQty.Equal(expectedQuote) {
		t.Errorf("sell quoteQty = %s, want %s (cost - fee)", resp.QuoteQty.String(), expectedQuote.String())
	}
	// Sell: Fee in quote (dollars)
	if !resp.Fee.Equal(fee) {
		t.Errorf("sell fee = %s, want %s", resp.Fee.String(), fee.String())
	}
}

func TestParseFill_Rejected(t *testing.T) {
	raw := []byte(`{"action":"rejected","reason":"insufficient balance"}`)

	c := &KrakenClient{log: zap.NewNop(), lastPrices: make(map[string]decimal.Decimal)}
	req := &TradeRequest{Pair: "BTC-USD", Side: "buy"}
	resp, err := c.parseFill(raw, req)
	if err == nil {
		t.Fatal("expected error for rejected action")
	}
	if resp.Status != StatusRejected {
		t.Errorf("status = %q, want %q", resp.Status, StatusRejected)
	}
}

func TestGetBalance_Paper(t *testing.T) {
	// Simulate the JSON that `kraken paper balance -o json` returns.
	raw := []byte(`{"balances":{"USD":{"available":9990.42,"reserved":0.0,"total":9990.42},"BTC":{"available":0.0001,"reserved":0.0,"total":0.0001},"ETH":{"available":0.001,"reserved":0.0,"total":0.001}},"mode":"paper"}`)

	var resp struct {
		Balances map[string]struct {
			Available float64 `json:"available"`
			Reserved  float64 `json:"reserved"`
			Total     float64 `json:"total"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Balances) != 3 {
		t.Errorf("expected 3 balances, got %d", len(resp.Balances))
	}
	usd, ok := resp.Balances["USD"]
	if !ok {
		t.Fatal("missing USD balance")
	}
	if usd.Available != 9990.42 {
		t.Errorf("USD available = %f, want 9990.42", usd.Available)
	}
}

func TestForAgent_PropagatesPairs(t *testing.T) {
	pairs := []string{"BTC-USD", "ETH-USD", "SOL-USD"}
	root := NewKrakenClient(zap.NewNop(), KrakenConfig{
		Sandbox:  true,
		StateDir: t.TempDir(),
		Pairs:    pairs,
	})

	child := root.ForAgent("agent-test")
	krakenChild, ok := child.(*KrakenClient)
	if !ok {
		t.Fatal("ForAgent should return *KrakenClient")
	}
	if len(krakenChild.pairs) != len(pairs) {
		t.Fatalf("child pairs = %v, want %v", krakenChild.pairs, pairs)
	}
	for i, p := range pairs {
		if krakenChild.pairs[i] != p {
			t.Errorf("child.pairs[%d] = %q, want %q", i, krakenChild.pairs[i], p)
		}
	}

	// Mutating root should not mutate child copy.
	root.pairs[0] = "MUTATED-PAIR"
	if krakenChild.pairs[0] != "BTC-USD" {
		t.Fatalf("child pair aliasing root slice: got %q, want BTC-USD", krakenChild.pairs[0])
	}
}

func TestForAgent_NoPairs(t *testing.T) {
	root := NewKrakenClient(zap.NewNop(), KrakenConfig{
		Sandbox:  true,
		StateDir: t.TempDir(),
	})

	child := root.ForAgent("agent-test")
	krakenChild := child.(*KrakenClient)
	if krakenChild.pairs != nil {
		t.Errorf("child pairs should be nil when root has no pairs, got %v", krakenChild.pairs)
	}
}

func TestGetFillHistory_RealMode_AllPairsFail(t *testing.T) {
	// Real mode with a non-existent binary - all pairs will fail.
	c := &KrakenClient{
		log:        zap.NewNop(),
		krakenBin:  "/nonexistent/kraken",
		sandbox:    false,
		pairs:      []string{"BTC-USD", "ETH-USD"},
		lastPrices: make(map[string]decimal.Decimal),
		agents:     make(map[string]*KrakenClient),
	}

	_, err := c.GetFillHistory("")
	if err == nil {
		t.Fatal("expected error when all pairs fail")
	}
	want := "all 2 pairs failed"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestGetFillHistory_PaperMode(t *testing.T) {
	// Paper mode with non-existent binary fails at run level, not pair iteration.
	// This verifies paper mode does NOT use the pairs loop.
	c := &KrakenClient{
		log:        zap.NewNop(),
		krakenBin:  "/nonexistent/kraken",
		sandbox:    true,
		pairs:      []string{"BTC-USD"},
		lastPrices: make(map[string]decimal.Decimal),
		agents:     make(map[string]*KrakenClient),
		homeDir:    t.TempDir(),
		inited:     true, // skip paper init
	}

	_, err := c.GetFillHistory("")
	if err == nil {
		t.Fatal("expected error from missing binary")
	}
	// Paper mode error should NOT mention "pairs failed"
	if strings.Contains(err.Error(), "pairs failed") {
		t.Errorf("paper mode should not iterate pairs, got: %s", err.Error())
	}
}

func TestParseFill_Error(t *testing.T) {
	raw := []byte(`{"error":"validation","message":"Insufficient USDC balance. Available: 0.00, Required: 2.04"}`)

	c := &KrakenClient{log: zap.NewNop(), lastPrices: make(map[string]decimal.Decimal)}
	req := &TradeRequest{Pair: "ETH-USDC", Side: "buy"}
	resp, err := c.parseFill(raw, req)
	if err == nil {
		t.Fatal("expected error for error response")
	}
	if resp.Status != StatusRejected {
		t.Errorf("status = %q, want %q", resp.Status, StatusRejected)
	}
}

func TestNormalizeKrakenHistoryID(t *testing.T) {
	ts := time.Unix(1_700_000_000, 123_000_000).UTC()

	tests := []struct {
		name  string
		entry krakenHistoryEntry
		check func(string) bool
	}{
		{
			name:  "prefers id",
			entry: krakenHistoryEntry{ID: "id-1", TradeID: "trade-1", OrderID: "order-1"},
			check: func(got string) bool { return got == "id-1" },
		},
		{
			name:  "falls back to trade_id",
			entry: krakenHistoryEntry{TradeID: "trade-1", OrderID: "order-1"},
			check: func(got string) bool { return got == "trade-1" },
		},
		{
			name:  "falls back to order_id",
			entry: krakenHistoryEntry{OrderID: "order-1"},
			check: func(got string) bool { return got == "order-1" },
		},
		{
			name:  "generates synthetic id when all ids missing",
			entry: krakenHistoryEntry{Pair: "XBTUSD", Side: "buy", Price: 70000, Volume: 0.1},
			check: func(got string) bool { return strings.HasPrefix(got, "synthetic:XBTUSD:buy:") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeKrakenHistoryID(tc.entry, ts)
			if !tc.check(got) {
				t.Fatalf("unexpected id: %q", got)
			}
		})
	}
}

func TestParseKrakenHistoryTime(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    time.Time
		wantErr bool
	}{
		{
			name: "rfc3339",
			raw:  "2026-04-11T22:33:44Z",
			want: time.Date(2026, 4, 11, 22, 33, 44, 0, time.UTC),
		},
		{
			name: "rfc3339nano",
			raw:  "2026-04-11T22:33:44.123456789Z",
			want: time.Date(2026, 4, 11, 22, 33, 44, 123456789, time.UTC),
		},
		{
			name: "unix-seconds-float",
			raw:  "1711829696.125",
			want: time.Unix(1711829696, 125000000).UTC(),
		},
		{
			name:    "invalid",
			raw:     "not-a-time",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseKrakenHistoryTime(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("time = %s, want %s", got.Format(time.RFC3339Nano), tc.want.Format(time.RFC3339Nano))
			}
		})
	}
}
