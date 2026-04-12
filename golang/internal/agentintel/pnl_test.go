package agentintel

import (
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

func TestGetPrice_UsesExecutionMinute(t *testing.T) {
	calc := &Calculator{log: zap.NewNop()}
	candles := []Candle{
		{T: 100, Close: "10.00", VWAP: "10.50"},
		{T: 160, Close: "11.00", VWAP: "11.50"},
		{T: 220, Close: "12.00", VWAP: "12.50"},
	}
	md := map[string][]Candle{"ETHUSD": candles}

	tests := []struct {
		name  string
		ts    int64
		want  string
		desc  string
	}{
		{"exact match", 160, "11.50", "VWAP of candle starting at 160"},
		{"between candles", 180, "12.50", "trade at 180, floored to 180, next candle at 220"},
		{"before all candles", 50, "10.50", "floored to 0, first candle at 100 within 10min"},
		{"just after last candle", 250, "12.50", "falls back to last candle at 220"},
		{"far after all candles", 900, "0", "no candle within 10min"},
		{"within candle minute", 161, "11.50", "floored to 120, candle at 160 is closest"},
		{"at candle boundary", 220, "12.50", "exact match at 220"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := calc.getPrice(md, "ETHUSD", tc.ts)
			want := decimal.RequireFromString(tc.want)
			if !got.Equal(want) {
				t.Errorf("getPrice(ts=%d) = %s, want %s (%s)", tc.ts, got, want, tc.desc)
			}
		})
	}
}

func TestGetPrice_StalenessLimit(t *testing.T) {
	calc := &Calculator{log: zap.NewNop()}
	candles := []Candle{
		{T: 700, Close: "10.00", VWAP: "10.50"},
	}
	md := map[string][]Candle{"ETHUSD": candles}

	// Trade 600 seconds before candle - within 10 min, should work.
	got := calc.getPrice(md, "ETHUSD", 100)
	if got.IsZero() {
		t.Error("ts=100, candle at 700 (600s gap) should return price, got zero")
	}

	// Trade 601 seconds before candle - too far.
	got = calc.getPrice(md, "ETHUSD", 99)
	if !got.IsZero() {
		t.Errorf("ts=99, candle at 700 (601s gap) should return zero, got %s", got)
	}
}

func TestGetPrice_VWAPFallbackToClose(t *testing.T) {
	calc := &Calculator{log: zap.NewNop()}
	candles := []Candle{
		{T: 100, Close: "10.00", VWAP: ""},  // no VWAP
		{T: 160, Close: "11.00", VWAP: "0"},  // zero VWAP
	}
	md := map[string][]Candle{"ETHUSD": candles}

	got := calc.getPrice(md, "ETHUSD", 100)
	if !got.Equal(decimal.RequireFromString("10.00")) {
		t.Errorf("empty VWAP should fallback to Close, got %s", got)
	}

	got = calc.getPrice(md, "ETHUSD", 160)
	if !got.Equal(decimal.RequireFromString("11.00")) {
		t.Errorf("zero VWAP should fallback to Close, got %s", got)
	}
}

func TestGetPrice_EmptyMarketData(t *testing.T) {
	calc := &Calculator{log: zap.NewNop()}
	md := map[string][]Candle{}

	got := calc.getPrice(md, "ETHUSD", 100)
	if !got.IsZero() {
		t.Errorf("no market data should return zero, got %s", got)
	}
}

func TestCandleDedup_KeepsLast(t *testing.T) {
	// Simulate what loadMarketData does: sort + dedup keeping last.
	candles := []Candle{
		{T: 100, Close: "10.00", VWAP: "10.50"},  // partial from previous sync
		{T: 100, Close: "10.20", VWAP: "10.70"},  // complete from current sync
		{T: 160, Close: "11.00", VWAP: "11.50"},
	}

	// Dedup logic from loadMarketData: keep last for each timestamp.
	deduped := candles[:0]
	for i := range candles {
		if i == len(candles)-1 || candles[i].T != candles[i+1].T {
			deduped = append(deduped, candles[i])
		}
	}

	if len(deduped) != 2 {
		t.Fatalf("expected 2 candles after dedup, got %d", len(deduped))
	}
	if deduped[0].Close != "10.20" {
		t.Errorf("should keep LAST candle for t=100, got Close=%s (want 10.20)", deduped[0].Close)
	}
	if deduped[1].T != 160 {
		t.Errorf("second candle should be t=160, got t=%d", deduped[1].T)
	}
}

func TestDedup_TradeIntents(t *testing.T) {
	items := []TradeIntent{
		{TxHash: "0xaaa", LogIndex: 1, Pair: "ETHUSD"},
		{TxHash: "0xbbb", LogIndex: 2, Pair: "BTCUSD"},
		{TxHash: "0xaaa", LogIndex: 1, Pair: "ETHUSD"}, // duplicate
		{TxHash: "0xccc", LogIndex: 3, Pair: "SOLUSD"},
	}

	result := dedup(items, func(t TradeIntent) string {
		return fmt.Sprintf("%s:%d", t.TxHash, t.LogIndex)
	})

	if len(result) != 3 {
		t.Errorf("dedup: got %d items, want 3", len(result))
	}
}

func TestDedup_ReputationFeedback(t *testing.T) {
	items := []ReputationFeedback{
		{TxHash: "0xaaa", LogIndex: 1, AgentID: 5, Score: 80},
		{TxHash: "0xaaa", LogIndex: 1, AgentID: 5, Score: 80}, // duplicate
		{TxHash: "0xbbb", LogIndex: 2, AgentID: 5, Score: 90},
	}

	result := dedup(items, func(r ReputationFeedback) string {
		return fmt.Sprintf("%s:%d", r.TxHash, r.LogIndex)
	})

	if len(result) != 2 {
		t.Errorf("dedup: got %d items, want 2", len(result))
	}
}

func TestCanonicalPair(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		// Already canonical
		{"XBTUSD", "XBTUSD"},
		{"ETHUSD", "ETHUSD"},
		{"SOLUSD", "SOLUSD"},

		// BTC -> XBT (Kraken convention)
		{"BTCUSD", "XBTUSD"},
		{"BTC-USD", "XBTUSD"},
		{"BTC/USD", "XBTUSD"},
		{"BTC/USDT", "XBTUSD"},
		{"BTC/USDC", "XBTUSD"},

		// WETH -> ETH
		{"WETH/USDC", "ETHUSD"},
		{"ETH-USD", "ETHUSD"},
		{"ETH/USD", "ETHUSD"},
		{"ETH/USDC", "ETHUSD"},

		// Dash and slash removal
		{"ZEC-USD", "ZECUSD"},
		{"SOL/USD", "SOLUSD"},
		{"AVAX-USD", "AVAXUSD"},
		{"LINK/USD", "LINKUSD"},
		{"FARTCOIN-USD", "FARTCOINUSD"},

		// USDT/USDC -> USD normalization
		{"DOGE-USDC", "DOGEUSD"},
		{"BNB/USDT", "BNBUSD"},
		{"SHIB-USDC", "SHIBUSD"},

		// Quoted strings
		{`"ETHUSD"`, "ETHUSD"},

		// Passthrough for unknown
		{"MONUSD", "MONUSD"},
		{"FARTCOINUSD", "FARTCOINUSD"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got := CanonicalPair(tc.raw)
			if got != tc.want {
				t.Errorf("CanonicalPair(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDetectGaming(t *testing.T) {
	tests := []struct {
		name       string
		agent      ComputedAgent
		wantFlags  int
		wantSubstr string
	}{
		{
			name: "attestation spam",
			agent: func() ComputedAgent {
				atts := make([]Attestation, 150)
				// Multiple validators = suspicious; single validator (contract) is normal.
				for i := range atts {
					atts[i].Validator = fmt.Sprintf("0x%040d", i%5)
				}
				return ComputedAgent{Attestations: atts}
			}(),
			wantFlags:  1,
			wantSubstr: "attestation_spam",
		},
		{
			name: "reputation sybil",
			agent: ComputedAgent{
				Summary: AgentSummary{
					RepFeedbackCount:    50,
					RepUniqueValidators: 50,
				},
			},
			wantFlags:  1,
			wantSubstr: "reputation_sybil",
		},
		{
			name: "stablecoin padding",
			agent: ComputedAgent{
				Summary: AgentSummary{
					PnLByPair: map[string]PairPnL{
						"USDTUSD": {BuyVolume: "8000.00", SellVolume: "8000.00"},
						"ETHUSD":  {BuyVolume: "2000.00", SellVolume: "2000.00"},
					},
				},
			},
			wantFlags:  1,
			wantSubstr: "stablecoin_padding",
		},
		{
			name: "clean agent",
			agent: ComputedAgent{
				Attestations: make([]Attestation, 5),
				Summary: AgentSummary{
					RepFeedbackCount:    3,
					RepUniqueValidators: 3,
					PnLByPair: map[string]PairPnL{
						"ETHUSD": {BuyVolume: "5000.00", SellVolume: "5000.00"},
					},
				},
			},
			wantFlags: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flags := detectGaming(tc.agent)
			if len(flags) != tc.wantFlags {
				t.Errorf("got %d flags %v, want %d", len(flags), flags, tc.wantFlags)
			}
			if tc.wantSubstr != "" && len(flags) > 0 {
				found := false
				for _, f := range flags {
					for i := 0; i <= len(f)-len(tc.wantSubstr); i++ {
						if f[i:i+len(tc.wantSubstr)] == tc.wantSubstr {
							found = true
							break
						}
					}
				}
				if !found {
					t.Errorf("flags %v don't contain %q", flags, tc.wantSubstr)
				}
			}
		})
	}
}
