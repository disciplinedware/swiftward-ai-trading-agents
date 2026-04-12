# Kraken Exchange Client

> **Status**: ✅ Shipped
> **Package**: `golang/internal/exchange/`
> **Implementation**: `kraken_client.go`

## What was built

A production Kraken exchange client that executes market orders by wrapping the native `kraken` CLI binary. Supports paper trading (sandboxed per-agent state) and live trading (real API). Implements per-agent HOME directory isolation, native exchange-level stop-loss and take-profit orders, and historical fill recovery for reconciliation after restarts. Each trade runs under a 15-second timeout; the global price cache is shared across agents since tickers are not agent-specific.

## Modes

- **Paper** (`sandbox=true`): no API key needed. Per-agent `HOME={StateDir}/{agentID}` so each agent has an isolated paper account. First trade for a new agent auto-initializes with `kraken paper init --balance 10000 --currency USD`.
- **Live** (`sandbox=false`): real trading via `kraken order ...`. Kraken CLI reads `KRAKEN_API_KEY` and `KRAKEN_API_SECRET` from environment.

## Interfaces implemented

- `Client` (base) - `SubmitTrade`, `GetPrice`, `GetPrices`
- `StopOrderProvider` - `PlaceStopOrder`, `CancelStopOrder` for native SL/TP (Tier 1 protection)
- `FillHistoryProvider` - `GetFillHistory(agentID)` for crash recovery
- `AgentClientProvider` - `ForAgent(agentID)` returns a scoped sub-client with its own HOME
- `BalanceProvider` - reads exchange balances via `kraken balance` or `kraken paper balance`

## How it works

1. `toKrakenPair` converts canonical pairs (`BTC-USD`) to Kraken format (`XBTUSD`)
2. Base quantity: use `Qty` directly if positive, otherwise compute from `Value / last_price`
3. Run CLI: `kraken paper {side} {pair} {qty}` (paper) or `kraken order {side} {pair} {qty} --type market` (live)
4. Parse the JSON response, extract fill price, volume, and fee (Kraken reports fees in quote currency)
5. Normalize fees: buy-side fees are converted to base units; sell-side fees stay in quote
6. Cache the fill price in `lastPrices` so subsequent `GetPrice` calls see live data
7. Return `TradeResponse` with normalized `Qty`, `QuoteQty`, `Fee`

The CLI runner (`run()`) always appends `-o json`, applies a 15-second timeout, and overrides `HOME` when scoped to an agent. Failed command stderr is bubbled up in the error message.

## Pair format

Kraken uses legacy codes for some assets:

| Canonical | Kraken | Rule |
|-----------|--------|------|
| BTC-USD | XBTUSD | BTC → XBT |
| DOGE-USD | XDGUSD | DOGE → XDG |
| ETH-USD | ETHUSD | direct |
| SOL-USDC | SOLUSDC | direct |
| BTC-USDT | XBTUSDT | BTC → XBT |
| ETH-BTC | ETHXBT | BTC in quote → XBT |

`fromKrakenPair` walks known quote suffixes in longest-first order (`USDT`, `USDC`, `USD`, `EUR`, `GBP`, `BTC`, `ETH`) to avoid partial matches.

## Key files

- `golang/internal/exchange/kraken_client.go` - main client (~600 lines), all interface implementations
- `golang/internal/exchange/kraken_client_test.go` - table-driven tests for pair conversion, fill parsing, balance parsing
- `golang/internal/exchange/client.go` - base `Client` interface and optional providers

## Tests

- `TestToKrakenPair` / `TestFromKrakenPair` - bidirectional pair mapping
- `TestParseFill_Buy` - buy with fee converted from quote (USD) to base (BTC); net qty after fee
- `TestParseFill_Sell` - sell delivers full volume, fee deducted from quote proceeds
- `TestParseFill_Rejected` - rejected trade returns `StatusRejected` with error message
- `TestGetBalance_Paper` - paper balance JSON `{"balances":{"USD":{...},"BTC":{...}}}` parsing

## Notes

- **Per-agent isolation** only applies when `StateDir` is configured. Without it, all agents share one Kraken account and one set of paper balances. With it, `ForAgent(agentID)` creates a scoped client with `HOME=StateDir/agentID`, and Kraken CLI reads / writes its state directory from that HOME.
- **Prices are global**: `lastPrices` lives on the root client, not per agent. Tickers are not agent-specific and sharing them avoids N duplicate refresh calls.
- **Fee normalization**: the `Client` interface contract is "buy fees in base, sell fees in quote". Kraken always reports fees in quote, so buy-side fees are converted to base by dividing by fill price.
- **Stop orders**: `PlaceStopOrder` wraps `kraken order add --type stop-loss` or `--type take-profit`. Some CLI versions return text instead of JSON, so the parser falls back to treating raw output as an order ID.
- **Fill recovery**: `GetFillHistory` queries `kraken paper history` (paper) or `kraken trades` (live), parses the JSON, and returns only filled trades (skips pending / cancelled). Used by reconciliation on startup to backfill the DB.
