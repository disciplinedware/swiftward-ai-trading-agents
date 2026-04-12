package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"ai-trading-agents/internal/marketdata"
)

// stubSource is a minimal DataSource for testing PaperClient.
type stubSource struct {
	tickers map[string]string // market -> last price string
	err     error
}

func (s *stubSource) GetTicker(_ context.Context, symbols []string) ([]marketdata.Ticker, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []marketdata.Ticker
	for _, sym := range symbols {
		if price, ok := s.tickers[sym]; ok {
			out = append(out, marketdata.Ticker{Market: sym, Last: price})
		}
	}
	return out, nil
}

func (s *stubSource) GetCandles(_ context.Context, _ string, _ marketdata.Interval, _ int, _ time.Time) ([]marketdata.Candle, error) {
	return nil, nil
}
func (s *stubSource) GetOrderbook(_ context.Context, _ string, _ int) (*marketdata.Orderbook, error) {
	return nil, nil
}
func (s *stubSource) GetMarkets(_ context.Context, _ string) ([]marketdata.MarketInfo, error) {
	return nil, nil
}
func (s *stubSource) GetFundingRates(_ context.Context, _ string, _ int) (*marketdata.FundingData, error) {
	return nil, nil
}
func (s *stubSource) GetOpenInterest(_ context.Context, _ string) (*marketdata.OpenInterest, error) {
	return nil, nil
}
func (s *stubSource) Name() string { return "stub" }

func TestPaperClient_GetPrice(t *testing.T) {
	log := zap.NewNop()

	tests := []struct {
		name      string
		tickers   map[string]string
		sourceErr error
		market    string
		wantOK    bool
		wantPrice string
	}{
		{
			name:      "no prior trade, source returns price",
			tickers:   map[string]string{"ETH-USDC": "2000.00"},
			market:    "ETH-USDC",
			wantOK:    true,
			wantPrice: "2000",
		},
		{
			name:    "unknown market, source returns nothing",
			tickers: map[string]string{"BTC-USDC": "65000"},
			market:  "ETH-USDC",
			wantOK:  false,
		},
		{
			name:      "source error returns false",
			sourceErr: errors.New("network error"),
			market:    "ETH-USDC",
			wantOK:    false,
		},
		{
			name:    "invalid price string returns false",
			tickers: map[string]string{"ETH-USDC": "not-a-number"},
			market:  "ETH-USDC",
			wantOK:  false,
		},
		{
			name:    "zero price returns false",
			tickers: map[string]string{"ETH-USDC": "0"},
			market:  "ETH-USDC",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &stubSource{tickers: tt.tickers, err: tt.sourceErr}
			c := NewPaperClient(source, log, decimal.NewFromFloat(0.001))

			got, ok := c.GetPrice(tt.market)
			if ok != tt.wantOK {
				t.Fatalf("GetPrice(%q) ok=%v, want %v", tt.market, ok, tt.wantOK)
			}
			if tt.wantOK && got.String() != decimal.RequireFromString(tt.wantPrice).String() {
				t.Errorf("GetPrice(%q) price=%s, want %s", tt.market, got, tt.wantPrice)
			}
		})
	}
}

func TestPaperClient_GetPrice_CachesAfterFetch(t *testing.T) {
	source := &stubSource{tickers: map[string]string{"ETH-USDC": "2000.00"}}
	c := NewPaperClient(source, zap.NewNop(), decimal.NewFromFloat(0.001))

	// First call: fetches from source.
	p1, ok1 := c.GetPrice("ETH-USDC")
	if !ok1 {
		t.Fatal("expected ok on first call")
	}

	// Poison the source — second call must use cache.
	source.err = errors.New("should not be called")

	p2, ok2 := c.GetPrice("ETH-USDC")
	if !ok2 {
		t.Fatal("expected ok on cached call")
	}
	if !p1.Equal(p2) {
		t.Errorf("cached price %s != fetched price %s", p2, p1)
	}
}

// TestPaperClient_SubmitTradeQty verifies that when TradeRequest.Qty is positive
// the exchange uses it directly as the base qty and ignores Value. This is the
// fix for the stop-loss drift bug where value/avgPrice would imply a sell qty
// larger than the actual holding once the live price had moved.
func TestPaperClient_SubmitTradeQty(t *testing.T) {
	cases := []struct {
		name            string
		side            string
		qty             string
		livePrice       string
		commissionRate  string
		wantQty         string // net qty in response
		wantQuoteQty    string
		wantFee         string
	}{
		{
			name:           "sell qty bypasses price round-trip during drawdown",
			side:           "sell",
			qty:            "0.01262451", // request 8dp, PaperClient rounds to 6dp
			livePrice:      "60000.00",   // SL fired, price below entry
			commissionRate: "0.001",
			// gross sold (rounded to 6dp): 0.012625 BTC
			// gross quote: 0.012625 * 60000 = 757.50
			// fee: 757.50 * 0.001 = 0.7575 → rounded(2) = 0.76
			// net quote: 757.50 * 0.999 = 756.7425 → rounded(2) = 756.74
			wantQty:      "0.012625",
			wantQuoteQty: "756.74",
			wantFee:      "0.76",
		},
		{
			name:           "buy qty fills exact base amount",
			side:           "buy",
			qty:            "0.5",
			livePrice:      "2500.00",
			commissionRate: "0.001",
			// gross bought: 0.5 ETH
			// fee in base: 0.5 * 0.001 = 0.0005 → rounded(6) = 0.0005
			// net qty: 0.5 - 0.0005 = 0.4995
			// quoteQty (full cash paid): 0.5 * 2500 = 1250.00
			wantQty:      "0.4995",
			wantQuoteQty: "1250",
			wantFee:      "0.0005",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := &stubSource{tickers: map[string]string{"BTC-USDC": tc.livePrice, "ETH-USDC": tc.livePrice}}
			c := NewPaperClient(source, zap.NewNop(), decimal.RequireFromString(tc.commissionRate))

			pair := "BTC-USDC"
			if tc.side == "buy" {
				pair = "ETH-USDC"
			}

			resp, err := c.SubmitTrade(&TradeRequest{
				Pair: pair,
				Side: tc.side,
				Qty:  decimal.RequireFromString(tc.qty),
				// Value intentionally zero — must be ignored when Qty is set.
			})
			if err != nil {
				t.Fatalf("SubmitTrade: %v", err)
			}
			if resp.Status != StatusFilled {
				t.Fatalf("status = %s, want %s", resp.Status, StatusFilled)
			}
			if resp.Qty.String() != decimal.RequireFromString(tc.wantQty).String() {
				t.Errorf("Qty = %s, want %s", resp.Qty, tc.wantQty)
			}
			if resp.QuoteQty.String() != decimal.RequireFromString(tc.wantQuoteQty).String() {
				t.Errorf("QuoteQty = %s, want %s", resp.QuoteQty, tc.wantQuoteQty)
			}
			if resp.Fee.String() != decimal.RequireFromString(tc.wantFee).String() {
				t.Errorf("Fee = %s, want %s", resp.Fee, tc.wantFee)
			}
		})
	}
}

// TestPaperClient_SubmitTradeQtyIgnoresValue is a regression for the original bug:
// before the fix, a sell submitted with value=qty*avgPrice would be re-divided
// by the (lower) live price and produce a qty larger than the holding. With Qty
// set, Value must be ignored entirely.
func TestPaperClient_SubmitTradeQtyIgnoresValue(t *testing.T) {
	source := &stubSource{tickers: map[string]string{"BTC-USDC": "60000.00"}}
	c := NewPaperClient(source, zap.NewNop(), decimal.Zero)

	resp, err := c.SubmitTrade(&TradeRequest{
		Pair:  "BTC-USDC",
		Side:  "sell",
		Qty:   decimal.RequireFromString("0.01"),
		Value: decimal.RequireFromString("999999999"), // would be 16666 BTC at 60k - must be ignored
	})
	if err != nil {
		t.Fatalf("SubmitTrade: %v", err)
	}
	if resp.Qty.String() != "0.01" {
		t.Errorf("Qty = %s, want 0.01 (Value must be ignored when Qty is set)", resp.Qty)
	}
}

func TestPaperClient_GetPrice_PopulatedByTrade(t *testing.T) {
	source := &stubSource{tickers: map[string]string{"ETH-USDC": "2000.00"}}
	c := NewPaperClient(source, zap.NewNop(), decimal.NewFromFloat(0.001))

	// Execute a trade which caches the price.
	_, err := c.SubmitTrade(&TradeRequest{
		Pair:  "ETH-USDC",
		Side:  "buy",
		Value: decimal.NewFromInt(500),
	})
	if err != nil {
		t.Fatalf("SubmitTrade: %v", err)
	}

	// GetPrice must now return the cached trade price without hitting source.
	source.err = errors.New("should not be called")
	_, ok := c.GetPrice("ETH-USDC")
	if !ok {
		t.Error("GetPrice should return cached price after trade")
	}
}
