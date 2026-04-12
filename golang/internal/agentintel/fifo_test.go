package agentintel

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func assertDecimal(t *testing.T, label string, got, want, epsilon decimal.Decimal) {
	t.Helper()
	diff := got.Sub(want).Abs()
	if diff.GreaterThan(epsilon) {
		t.Errorf("%s = %s, want %s (diff %s > epsilon %s)", label, got.StringFixed(6), want.StringFixed(6), diff.StringFixed(6), epsilon.StringFixed(6))
	}
}

var eps = d("0.01")

// Fee model: Kraken charges 0.26% in USD (quote) for both buy and sell.
// BUY: you get full qty, pay more USD. CostPerUnit = price * 1.0026.
// SELL: you sell full qty, receive less USD. Proceeds = price * 0.9974.

func TestFIFO_BuyOpenLong(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	r := f.Buy(d("10"), d("100"))

	// Fee: 10*100*0.0026 = $2.60.
	assertDecimal(t, "fee", r.FeeUSD, d("2.60"), eps)
	// Full qty received (fee is in USD, not base).
	assertDecimal(t, "net_qty", r.NetQty, d("10"), eps)
	assertDecimal(t, "position", f.Position(), d("10"), eps)
	// Cost basis: 100 * 1.0026 = $100.26/unit.
	assertDecimal(t, "avg_cost", f.AvgCost(), d("100.26"), eps)
}

func TestFIFO_SellCloseLong_Profit(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("10"), d("100")) // 10 units, cost $100.26/unit
	r := f.Sell(d("10"), d("110"))

	// Sell 10 @ $110. Net price = 110 * 0.9974 = $109.714/unit.
	// PnL per unit = 109.714 - 100.26 = 9.454.
	// Total PnL = 9.454 * 10 = 94.54.
	assertDecimal(t, "pnl", r.RealizedPnL, d("94.54"), d("0.10"))
	if !r.IsClosing {
		t.Error("should be closing")
	}
	// Position flat after selling full qty.
	if !f.IsFlat() {
		t.Errorf("should be flat, got %s", f.Position())
	}
}

func TestFIFO_SellCloseLong_Loss(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("10"), d("100"))
	r := f.Sell(d("10"), d("90"))

	// Net sell price = 90 * 0.9974 = 89.766.
	// PnL = (89.766 - 100.26) * 10 = -104.94.
	assertDecimal(t, "pnl", r.RealizedPnL, d("-104.94"), d("0.10"))
}

func TestFIFO_SellCloseLong_SamePrice_FeeLoss(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("10"), d("100"))
	r := f.Sell(d("10"), d("100"))

	// Buy cost: 100.26/unit. Sell proceeds: 99.74/unit.
	// PnL = (99.74 - 100.26) * 10 = -5.20 (double fee: 0.26% buy + 0.26% sell).
	assertDecimal(t, "pnl", r.RealizedPnL, d("-5.20"), d("0.01"))
}

func TestFIFO_PartialSellTakesFirstLot(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("5"), d("100")) // lot 1: 5 @ $100.26
	f.Buy(d("5"), d("200")) // lot 2: 5 @ $200.52

	r := f.Sell(d("4"), d("150"))

	// Sell 4 from lot 1. Cost = 4 * 100.26 = 401.04.
	// Proceeds = 4 * 150 * 0.9974 = 598.44.
	// PnL = 598.44 - 401.04 = 197.40.
	assertDecimal(t, "pnl", r.RealizedPnL, d("197.40"), d("0.10"))
	// Remaining: 1 from lot1 + 5 from lot2 = 6.
	assertDecimal(t, "position", f.Position(), d("6"), d("0.01"))
}

func TestFIFO_SellMoreThanHeld_Reversal(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("5"), d("100")) // 5 @ $100.26

	r := f.Sell(d("10"), d("110"))

	// Close 5 units. Open short for remaining 5.
	if !r.IsReversal {
		t.Error("should be reversal")
	}
	if !f.Position().IsNegative() {
		t.Errorf("should be short, got %s", f.Position())
	}
	// PnL on closed 5: (110*0.9974 - 100.26) * 5 = (109.714 - 100.26) * 5 = 47.27.
	assertDecimal(t, "pnl", r.RealizedPnL, d("47.27"), d("0.20"))
}

func TestFIFO_ShortOpen(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	r := f.Sell(d("10"), d("100"))

	if !r.IsShortOpen {
		t.Error("should be short open")
	}
	// Full qty goes short. CostPerUnit = 100 * 0.9974 = 99.74 (net proceeds per unit).
	assertDecimal(t, "position", f.Position(), d("-10"), eps)
	assertDecimal(t, "avg_cost", f.AvgCost(), d("99.74"), eps)
}

func TestFIFO_ShortCoverProfit(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Sell(d("10"), d("100")) // short 10 @ net $99.74/unit
	r := f.Buy(d("10"), d("90"))

	// Cover cost = 90 * 1.0026 = $90.234/unit.
	// PnL = (99.74 - 90.234) * 10 = 95.06.
	if !r.IsClosing {
		t.Error("should be closing")
	}
	assertDecimal(t, "pnl", r.RealizedPnL, d("95.06"), d("0.10"))
}

func TestFIFO_ShortCoverLoss(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Sell(d("10"), d("100"))
	r := f.Buy(d("10"), d("110"))

	// Cover cost = 110 * 1.0026 = $110.286/unit.
	// PnL = (99.74 - 110.286) * 10 = -105.46.
	assertDecimal(t, "pnl", r.RealizedPnL, d("-105.46"), d("0.10"))
}

func TestFIFO_ShortPartialCover(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Sell(d("10"), d("100")) // short 10 @ $99.74
	r := f.Buy(d("4"), d("90"))

	// Cover 4 units. PnL = (99.74 - 90.234) * 4 = 38.024.
	assertDecimal(t, "pnl", r.RealizedPnL, d("38.02"), d("0.10"))
	// Remaining: -6 (still short).
	assertDecimal(t, "remaining", f.Position(), d("-6"), d("0.01"))
}

func TestFIFO_ShortOvercoverFlipsToLong(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Sell(d("5"), d("100")) // short 5 @ $99.74
	r := f.Buy(d("10"), d("90"))

	if !r.IsReversal {
		t.Error("should be reversal")
	}
	if !f.Position().IsPositive() {
		t.Errorf("should be long, got %s", f.Position())
	}
	// Short PnL on 5 units: (99.74 - 90.234) * 5 = 47.53.
	if r.RealizedPnL.IsNegative() {
		t.Errorf("should be profit, got %s", r.RealizedPnL)
	}
}

func TestFIFO_TwoLotsFIFO(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("5"), d("100")) // lot 1: 5 @ $100.26
	f.Buy(d("5"), d("200")) // lot 2: 5 @ $200.52

	r := f.Sell(d("6"), d("150"))

	// Sells 5 from lot1 + 1 from lot2.
	// Cost = 5*100.26 + 1*200.52 = 501.30 + 200.52 = 701.82.
	// Proceeds = 6 * 150 * 0.9974 = 897.66.
	// PnL = 897.66 - 701.82 = 195.84.
	assertDecimal(t, "pnl", r.RealizedPnL, d("195.84"), d("0.10"))
	// Remaining: 4 in lot 2.
	assertDecimal(t, "position", f.Position(), d("4"), d("0.01"))
}

func TestFIFO_FullRoundTrip_LongThenShortThenCover(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))

	// 1. Buy 10 @ $100.
	f.Buy(d("10"), d("100"))
	assertDecimal(t, "after_buy_pos", f.Position(), d("10"), eps)

	// 2. Sell 20 @ $110 - closes long + opens short.
	r1 := f.Sell(d("20"), d("110"))
	if !r1.IsReversal {
		t.Error("step 2 should be reversal")
	}
	if !f.Position().IsNegative() {
		t.Errorf("step 2: should be short, got %s", f.Position())
	}
	// Long close PnL: (110*0.9974 - 100.26) * 10 = 94.54.
	assertDecimal(t, "long_close_pnl", r1.RealizedPnL, d("94.54"), d("0.50"))

	// 3. Buy 15 @ $105 - covers short + opens long.
	r2 := f.Buy(d("15"), d("105"))
	if !r2.IsReversal {
		t.Error("step 3 should be reversal")
	}
	if !f.Position().IsPositive() {
		t.Errorf("step 3: should be long, got %s", f.Position())
	}
	// Short was opened at net $109.714, covered at $105.273 -> profit.
	if r2.RealizedPnL.IsNegative() {
		t.Errorf("step 3: short cover should profit, got %s", r2.RealizedPnL)
	}
}

func TestFIFO_ZeroQtyTrade(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	r := f.Buy(d("0"), d("100"))
	assertDecimal(t, "fee", r.FeeUSD, d("0"), eps)
	assertDecimal(t, "position", f.Position(), d("0"), eps)
}

func TestFIFO_DustCleanup(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("1"), d("100"))
	// Sell exactly 1 unit (no fee qty reduction now).
	f.Sell(d("1"), d("100"))
	if !f.IsFlat() {
		t.Errorf("should be flat, got %s", f.Position().StringFixed(8))
	}
}

func TestFIFO_MultipleBuysOneSell(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("3"), d("100"))
	f.Buy(d("3"), d("110"))
	f.Buy(d("3"), d("120"))

	// Sell 9 = full position. Cap applies (overshoot 0 = exact match).
	r := f.Sell(d("9"), d("115"))

	// Cost: 3*100.26 + 3*110.286 + 3*120.312 = 300.78 + 330.858 + 360.936 = 992.574.
	// Proceeds: 9 * 115 * 0.9974 = 1032.306.
	// PnL = 1032.306 - 992.574 = 39.73.
	assertDecimal(t, "pnl", r.RealizedPnL, d("39.73"), d("1.00"))
}

func TestFIFO_PositionAndCostMethods(t *testing.T) {
	f := NewFIFOTracker(d("0.0026"))
	f.Buy(d("10"), d("100"))
	f.Buy(d("10"), d("200"))

	// Position: 10 + 10 = 20 (full qty, no fee reduction).
	assertDecimal(t, "position", f.Position(), d("20"), eps)
	// Cost: 10*100.26 + 10*200.52 = 1002.6 + 2005.2 = 3007.8.
	assertDecimal(t, "total_cost", f.TotalCost(), d("3007.8"), d("0.1"))
	// Avg: 3007.8 / 20 = 150.39.
	assertDecimal(t, "avg_cost", f.AvgCost(), d("150.39"), d("0.1"))
}
