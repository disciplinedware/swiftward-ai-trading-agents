## Why

The `PaperEngine` currently owns both execution logic (computing fill prices) and all portfolio accounting (reading balance state, building domain objects, writing to DB). This means the `LiveEngine` — which does real on-chain execution — never writes to the DB, so `get_portfolio`, `get_positions`, and `get_daily_pnl` always return empty state in live mode. The engine layer needs to be a thin execution boundary; portfolio persistence must live in the services layer and run regardless of which engine is active.

## What Changes

- **PaperEngine** stripped down to: compute fill price (with slippage), generate `paper_xxx` tx hash, return `ExecutionResult`. No DB access, no domain object construction.
- **LiveEngine** unchanged in structure — already only returns `ExecutionResult`.
- **PortfolioService** gains `record_open(intent, result, current_price)` and `record_close(asset, result, current_price)` methods that build Position/Trade/Snapshot objects and write them atomically.
- **TradingService** orchestrates the full flow: capacity pre-check → price fetch → engine execute → portfolio record. DB writes now happen in live mode too.
- `ExecutionResult` gains an optional `size_usd` field so the engine can communicate the actual filled size back to the recording layer.
- The `try_open_position` atomic capacity check moves into `record_open` so the lock is still held around count + write.

## Capabilities

### New Capabilities
- `engine-execution-protocol`: Defines the thin Engine interface contract — engines return fill price + tx hash only; no DB access permitted.

### Modified Capabilities
- `trading-paper-engine`: Engine no longer writes to DB; execution logic only.
- `trading-mcp-live-engine`: Now produces DB records via TradingService after on-chain confirmation.
- `trading-portfolio-state`: `PortfolioService` gains `record_open` / `record_close` write methods; existing read API unchanged.
- `trading-mcp-server`: `TradingService.execute_swap` gains capacity pre-check and post-execution DB recording step.

## Impact

- `python/src/trading_mcp/engine/paper.py` — major simplification (~15 lines of execution logic)
- `python/src/trading_mcp/engine/live.py` — no changes needed
- `python/src/trading_mcp/engine/result.py` — add optional `size_usd: Decimal | None`
- `python/src/trading_mcp/service/portfolio_service.py` — add `record_open`, `record_close`
- `python/src/trading_mcp/service/trading_service.py` — add capacity check + post-execute recording
- `python/src/trading_mcp/server.py` — no changes (wiring unchanged)
- Tests for `PaperEngine` and `TradingService` need updating
