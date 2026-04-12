# agent-intel - Design Decisions

This document explains key architectural and implementation choices. Read this before reviewing the code.

## Architecture

Three-step pipeline: `sync` (download) -> `calculate` (PnL) -> `generate` (HTML).

Between calculate and generate, an optional LLM analysis step runs via `/agent-intel-analyze` skill. This produces per-agent markdown files that get embedded in the generated HTML.

## Gaming Detection: Go flags vs LLM analysis

Gaming detection is split into two layers:

1. **Go code (`detectGaming` in pnl.go)**: Simple threshold-based flags (attestation count > 100, reputation sybil pattern, stablecoin volume > 40%). These are fast, deterministic, and shown as badges on the leaderboard. They are intentionally narrow - just enough to flag obvious patterns.

2. **LLM skill (`/agent-intel-analyze`)**: Deep, nuanced analysis per agent. Reads raw blockchain data (validator addresses, attestation notes, trade patterns, wallet nonces) and produces a written assessment. This is where sybil nonce checks, wash-trade timing analysis, volume anomaly detection, and strategy classification happen. The LLM has access to both `raw/` and `computed/` data directories.

Why split: Deterministic flags run every time without cost. LLM analysis is expensive, requires human review, and produces richer output than any rule-based system could. They serve different purposes on the site.

## Fee Model

Uniform 0.26% taker fee for all agents. Rationale:

- Kraken paper trading returns `"action":"market_order_filled"` - all trades are market/taker orders
- On-chain `maxSlippageBps=100` (1%) confirms market orders, not limit
- At hackathon volumes (< $50K per agent), everyone is at Kraken's lowest tier (0.26% taker)
- Our own exchange client (`golang/internal/exchange/paper_client.go`) confirms this model
- Kraken always charges fee in USD (quote currency) for both buy and sell. Verified via live Kraken CLI.
- **BUY**: you pay more USD, receive full qty. Cost basis = `price * (1 + feeRate)`.
- **SELL**: you receive less USD, sell full qty. Proceeds = `price * (1 - feeRate)`.
This matches actual Kraken paper trading behavior

Alternative considered: side-dependent fee (base on buy, quote on sell) as in algosmithy. Rejected because the economic effect is identical and the current model is simpler.

With the quote-currency fee model, buy gives exactly the requested qty (no fee reduction). Selling the same qty back matches exactly. No sell-side capping is needed. If an agent sells more than it holds, the excess opens a short position - this is calculated precisely, not approximated.

## Price Lookup: VWAP, no look-ahead

Trade prices use the VWAP of the 1-minute candle at or after the trade timestamp. The on-chain intent is posted before the Kraken execution, so the candle during or after the intent is the best approximation of the actual fill price.

- VWAP (volume-weighted average price) is standard for fill estimation in TradFi
- Candles are aggregated from Kraken's raw Trades endpoint, not OHLC (which caps at 720 candles)
- 10-minute staleness limit: if no candle within 10 minutes of the trade, price returns zero and the trade is skipped in PnL calculation
- Falls back to the most recent candle only when the trade is after all available data (end-of-dataset edge case)
- Candle close was considered but rejected: close is the last trade in the minute, potentially 59 seconds after the agent's trade

## FIFO Lot Accounting

Standalone `FIFOTracker` in `fifo.go` with `Buy()`/`Sell()` methods. Design borrowed from `algosmithy-core`'s `SymbolPosition`.

Key behaviors:
- Lots use signed quantity: positive = long, negative = short
- Position reversal in one trade: selling more than held closes the long and opens a short for the remainder (and vice versa)
- Dust threshold: lots below 0.0000001 are cleaned up to prevent accumulation of micro-positions from fee rounding
- Short PnL: `(open_price - cover_price * (1+fee)) * qty` - fee is included in the effective cover price

## Trade Execution Flow

The RiskRouter is a gate, not a log:
1. Agent calls `submitTradeIntent()` on RiskRouter
2. Router checks limits (position size, trades/hour)
3. Router emits `TradeApproved` or `TradeRejected` in the same tx
4. Only AFTER approval does the agent execute on Kraken

All three events (`TradeIntentSubmitted`, `TradeApproved`/`TradeRejected`) are in the same transaction.

## UNKNOWN Trade Outcomes

Trade intents without a matching approval/rejection event are UNKNOWN - a sync data gap, not a deliberate state. They are excluded from PnL (`outcome != "APPROVED"`) because without approval we can't confirm the trade executed on Kraken.

The sync gap happens when Infura rate-limits our event queries mid-chunk: we get the intent events but miss the approval events from the same block range. Fix: re-run sync which will re-query those blocks, or add per-tx receipt fetching for unmatched intents.

## Incremental Sync

- Block cursor (`meta.json:last_synced_block`) tracks sync progress
- JSONL files are append-only; deduplication happens on read (by `txHash:logIndex`) to handle crash+rerun
- Attestations are fetched in full each sync (contract returns the whole array) and overwritten
- Market data uses Kraken Trades endpoint `last` cursor (nanosecond) for pagination
- First sync starts from a fixed block (`10580000`, before hackathon agent registrations) to ensure full history regardless of when the tool is first run
- Candle dedup uses `SliceStable` sort so appended (newer/complete) candles win over older (partial) ones for the same timestamp

## Storage: Flat Files

SQLite was considered but rejected:
- Dataset is small (51 agents, 3 days, 21 pairs)
- JSON/JSONL files are human-readable and git-friendly
- No query joins needed - data is processed linearly
- Static site generator reads files directly
- Easier for LLM analysis skill to read

## Website: Go Templates (not SPA)

Pre-rendered static HTML via Go `html/template`. No client-side JS framework.
- One `index.html` (leaderboard) + one `agents/{id}.html` per agent
- Fastest possible page loads, works without JS
- Hugo rejected as overkill for generated tabular data
- React/Vite rejected as adding unnecessary build complexity
