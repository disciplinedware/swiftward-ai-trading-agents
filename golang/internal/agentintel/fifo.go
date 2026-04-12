package agentintel

import "github.com/shopspring/decimal"

// dustThreshold is the minimum qty to keep in a lot. Below this, the lot is consumed.
var dustThreshold = decimal.RequireFromString("0.0000001")

// FIFOLot represents a single purchase lot in the FIFO queue.
type FIFOLot struct {
	Qty         decimal.Decimal // units of the asset (positive=long, negative=short)
	CostPerUnit decimal.Decimal // execution price per unit
}

// FIFOTracker tracks a position in a single asset using FIFO lot accounting.
// Positive lots = long, negative lots = short.
// All monetary values use decimal.Decimal for precision.
type FIFOTracker struct {
	Lots    []FIFOLot
	FeeRate decimal.Decimal
}

// TradeResult is the output of a Buy or Sell operation.
type TradeResult struct {
	// NetQty is the net position change: positive for buys, negative for sells.
	// For sell reversals, reflects only the closed portion (not the short-opening remainder).
	// Not used for cash calculations - use the original qty for that.
	NetQty decimal.Decimal
	// FeeUSD is the fee amount in USD.
	FeeUSD decimal.Decimal
	// RealizedPnL is the PnL from closing existing lots. Zero if only opening.
	RealizedPnL decimal.Decimal
	// IsClosing is true if any existing lots were consumed.
	IsClosing bool
	// IsShortOpen is true if a new short position was opened (sell with no long lots).
	IsShortOpen bool
	// IsReversal is true if the trade flipped position direction (long->short or short->long).
	IsReversal bool
}

// NewFIFOTracker creates a tracker with the given fee rate (e.g., 0.0026 for 0.26%).
func NewFIFOTracker(feeRate decimal.Decimal) *FIFOTracker {
	return &FIFOTracker{FeeRate: feeRate}
}

// Buy processes a buy trade. qty is the raw quantity (before fees), price is the execution price.
//
// If the position is short, this covers the short first (FIFO), with any remainder opening a long.
// If the position is long or flat, this adds a new long lot.
//
// Buy fee: Kraken charges fee in USD (quote). You pay more USD, receive full qty.
// Cost basis per unit = price * (1 + feeRate).
func (f *FIFOTracker) Buy(qty, price decimal.Decimal) TradeResult {
	if qty.IsZero() || price.IsZero() {
		return TradeResult{}
	}
	amountUSD := qty.Mul(price)
	feeUSD := amountUSD.Mul(f.FeeRate)
	result := TradeResult{FeeUSD: feeUSD, NetQty: qty}

	// Effective cost per unit includes the fee.
	costPerUnit := price.Mul(oneVal.Add(f.FeeRate))

	if f.isShort() {
		// Cover short lots via FIFO.
		result.IsClosing = true
		remaining := qty

		for len(f.Lots) > 0 && remaining.IsPositive() {
			lot := &f.Lots[0]
			if lot.Qty.IsPositive() {
				break // hit a long lot
			}
			lotAbs := lot.Qty.Abs()
			used := decimal.Min(lotAbs, remaining)

			// Short PnL = (short_open_price - cover_cost_per_unit) * qty.
			pnl := used.Mul(lot.CostPerUnit.Sub(costPerUnit))
			result.RealizedPnL = result.RealizedPnL.Add(pnl)

			lot.Qty = lot.Qty.Add(used) // move toward zero
			remaining = remaining.Sub(used)
			if lot.Qty.Abs().LessThanOrEqual(dustThreshold) {
				f.Lots = f.Lots[1:]
			}
		}

		// If we bought more than the short, open a long for the remainder.
		if remaining.IsPositive() {
			result.IsReversal = true
			f.Lots = append(f.Lots, FIFOLot{Qty: remaining, CostPerUnit: costPerUnit})
		}
	} else {
		// Open or add to long. Full qty received, cost basis includes fee.
		f.Lots = append(f.Lots, FIFOLot{Qty: qty, CostPerUnit: costPerUnit})
	}

	return result
}

// Sell processes a sell trade. qty is the raw quantity (before fees), price is the execution price.
//
// If the position is long, this closes long lots via FIFO, with any remainder opening a short.
// If the position is short or flat, this adds a new short lot.
//
// Sell fee: Kraken charges fee in USD (quote). You sell full qty, receive less USD.
// Effective proceeds per unit = price * (1 - feeRate).
func (f *FIFOTracker) Sell(qty, price decimal.Decimal) TradeResult {
	if qty.IsZero() || price.IsZero() {
		return TradeResult{}
	}
	amountUSD := qty.Mul(price)
	feeUSD := amountUSD.Mul(f.FeeRate)
	result := TradeResult{FeeUSD: feeUSD}

	// Effective proceeds per unit after fee.
	netPricePerUnit := price.Mul(oneVal.Sub(f.FeeRate))

	if f.isLong() {
		// Close long lots via FIFO.
		result.IsClosing = true

		remaining := qty
		costOfSold := decimal.Zero

		for len(f.Lots) > 0 && remaining.IsPositive() {
			lot := &f.Lots[0]
			if lot.Qty.IsNegative() {
				break
			}
			used := decimal.Min(lot.Qty, remaining)
			costOfSold = costOfSold.Add(used.Mul(lot.CostPerUnit))
			lot.Qty = lot.Qty.Sub(used)
			remaining = remaining.Sub(used)
			if lot.Qty.LessThanOrEqual(dustThreshold) {
				f.Lots = f.Lots[1:]
			}
		}

		// PnL = net proceeds - cost basis. Full qty sold, fee reduces USD received.
		closedQty := qty.Sub(remaining)
		closedProceeds := closedQty.Mul(netPricePerUnit)
		result.RealizedPnL = closedProceeds.Sub(costOfSold)
		result.NetQty = closedQty.Neg() // negative = removed from position

		// If we sold more than we held, open a short for the remainder.
		if remaining.IsPositive() {
			result.IsReversal = true
			// Short: you sell full qty, effective open price is net of fee.
			f.Lots = append(f.Lots, FIFOLot{Qty: remaining.Neg(), CostPerUnit: netPricePerUnit})
		}
	} else {
		// Open or add to short position. Effective open price is net of fee.
		result.IsShortOpen = true
		f.Lots = append(f.Lots, FIFOLot{Qty: qty.Neg(), CostPerUnit: netPricePerUnit})
		result.NetQty = qty.Neg()
	}

	return result
}

// Position returns the net position quantity (positive=long, negative=short, zero=flat).
func (f *FIFOTracker) Position() decimal.Decimal {
	total := decimal.Zero
	for _, lot := range f.Lots {
		total = total.Add(lot.Qty)
	}
	return total
}

// TotalCost returns the total cost basis (absolute) of all remaining lots.
func (f *FIFOTracker) TotalCost() decimal.Decimal {
	total := decimal.Zero
	for _, lot := range f.Lots {
		total = total.Add(lot.Qty.Abs().Mul(lot.CostPerUnit))
	}
	return total
}

// AvgCost returns the average cost per unit. Zero if no position.
func (f *FIFOTracker) AvgCost() decimal.Decimal {
	pos := f.Position().Abs()
	if pos.LessThanOrEqual(dustThreshold) {
		return decimal.Zero
	}
	return f.TotalCost().Div(pos)
}

// IsFlat returns true if the position is effectively zero.
func (f *FIFOTracker) IsFlat() bool {
	return f.Position().Abs().LessThanOrEqual(dustThreshold)
}

func (f *FIFOTracker) isLong() bool {
	return f.Position().IsPositive()
}

func (f *FIFOTracker) isShort() bool {
	return f.Position().IsNegative()
}

var oneVal = decimal.NewFromInt(1)
