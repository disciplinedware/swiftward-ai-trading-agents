## Why

Task 7 created the DB schema. Task 8 adds the read/write service layer on top of it: a `PortfolioService` that exposes all portfolio query tools needed by trading-mcp (get_portfolio, get_positions, get_position, get_daily_pnl). Tasks 9–11 depend on this layer to read and write positions.

## What Changes

- Create `src/trading_mcp/domain/portfolio_service.py` — async service class with all query methods
- PnL arithmetic: unrealized PnL from current vs entry price; realized PnL stored in position row; daily PnL sums today's closed positions; drawdown vs rolling peak from portfolio_snapshots
- Concurrent write safety: asyncio.Lock wrapping all mutations
- Tests: PnL arithmetic, drawdown calculation, concurrent write safety, daily PnL reset at UTC midnight boundary

## Capabilities

### New Capabilities
- `trading-portfolio-state`: Portfolio read/write service for trading-mcp. Covers get_portfolio (balance + positions + realized PnL + drawdown + open count), get_positions (open positions with unrealized PnL), get_position (single asset), get_daily_pnl (resets UTC midnight), and atomic position open/close writes with asyncio.Lock.

### Modified Capabilities

## Impact

- New file `src/trading_mcp/domain/portfolio_service.py`
- New test file `tests/trading_mcp/test_portfolio_service.py`
- No existing code modified
