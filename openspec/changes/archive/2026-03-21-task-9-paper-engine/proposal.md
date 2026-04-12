## Why

Task 8 added portfolio read/write. Task 9 adds the `execute_swap` tool — the paper execution engine that accepts a `TradeIntent`, fills at current market price + 0.1% slippage, writes position + trade + portfolio_snapshot rows atomically, and returns execution result. This is the core trading action that all loops eventually call.

## What Changes

- Create `src/trading_mcp/engine/paper.py` — `PaperEngine` class implementing `execute_swap`
- Accepts `TradeIntent`, validates fields, fetches current price from price_feed-mcp (via HTTP)
- Applies 0.1% slippage to fill price
- LONG: opens new position (respects max_concurrent_positions limit)
- FLAT: closes the open position for the asset (if any)
- Returns `ExecutionResult` with status, tx_hash (paper_<uuid>), executed_price, slippage_pct
- Writes `Position`, `Trade`, `PortfolioSnapshot` atomically via `PortfolioService`

## Capabilities

### New Capabilities
- `trading-paper-engine`: Paper execution engine for trading-mcp. Fills LONG at entry+slippage, closes FLAT at current price. Enforces max concurrent position limit. Writes position, trade, and snapshot rows atomically. Returns structured ExecutionResult.

### Modified Capabilities

## Impact

- New directory `src/trading_mcp/engine/`
- New files: `engine/__init__.py`, `engine/paper.py`, `engine/result.py`
- New test file `tests/trading_mcp/test_paper_engine.py`
- Depends on `PortfolioService` (Task 8) and `TradeIntent` model (Task 2)
