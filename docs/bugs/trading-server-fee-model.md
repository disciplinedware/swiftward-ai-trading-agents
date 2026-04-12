# Trading Server Fee Model Bug

## Status: CONFIRMED, NOT FIXED

Found during agent-intel leaderboard development. Confirmed via live Kraken CLI testing.

## Root Cause

`parseFill` in `kraken_client.go:269-270` converts Kraken's USD fee to a fictional base-currency fee for buys:

```go
feeInBase := feeQuote.Div(fillPrice).Round(8) // invents a ZEC fee
qty = grossQty.Sub(feeInBase)                  // pretends you got less ZEC
```

But Kraken charges fee in USD for both buy and sell. You receive the full qty and pay more USD.

## Bugs

### 1. Buy cash delta missing the fee
`cashDelta = fillValue.Neg()` where `fillValue = cost` (not cost+fee). Cash is overstated by ~0.26% per buy trade.

### 2. Position qty understated
Stored qty = grossQty - feeInBase = 0.9974 ZEC when you actually hold 1.0 ZEC.

### 3. Fee asset reported as base when it's quote
Evidence records say `fee_asset: "ZEC"` but Kraken charged USD.

### 4. Sell PnL uses inflated AvgPrice
AvgPrice is approximately correct (fee baked in) but qty is wrong, causing PnL to be slightly off.

## Impact

- Cash overstated by ~$0.13 per $50 buy, compounds across all buys
- Position sizing, drawdown checks, risk limits all use overstated cash
- Dashboard shows 0.26% less position than actual holdings
- Evidence/audit trail has wrong fee currency

## Fix

In `parseFill`, change buy case:
```go
if req.Side == "buy" {
    qty = grossQty           // full qty received
    quoteQty = cost.Add(feeQuote) // total cash paid including fee
    fee = feeQuote           // fee in USD (as Kraken charges it)
}
```

In `service.go`:
```go
cashDelta = fillValue.Neg()  // fillValue now includes fee
feeAsset = pairQuote(pair)   // always USD
feeValue = resp.Fee          // already in USD, no conversion
```

Update `parseFill` tests to verify:
- Buy: Qty = gross volume (no reduction), QuoteQty = cost + fee, Fee in USD
- Sell: unchanged (already correct)

## Verified By

Live Kraken CLI test:
```
$ kraken paper buy ZECUSD 0.01 -o json
{"cost":3.32,"fee":0.00863,"volume":0.01,"price":331.98}
fee/cost = 0.2600% -> fee is in USD
```
