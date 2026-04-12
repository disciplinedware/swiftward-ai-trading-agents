## ADDED Requirements

### Requirement: get_portfolio returns full state
The system SHALL provide `PortfolioService.get_portfolio(current_prices)` returning a `PortfolioSummary` with: total_usd, stablecoin_balance, open_position_count, realized_pnl_today, current_drawdown_pct, and a list of open `PositionView` with unrealized PnL.

#### Scenario: Empty portfolio
- **WHEN** no positions exist and no snapshots exist
- **THEN** get_portfolio returns total_usd=starting_balance, open_position_count=0, drawdown=0

#### Scenario: Portfolio with open position
- **WHEN** one open LONG position exists with entry_price=100 and current_price=110
- **THEN** get_portfolio includes that position with unrealized_pnl_usd=(110-100)*size_usd/100

### Requirement: get_positions returns open positions with unrealized PnL
The system SHALL provide `PortfolioService.get_positions(current_prices)` returning a list of `PositionView` for all open positions, each with unrealized_pnl_usd and unrealized_pnl_pct computed from current price vs entry price.

#### Scenario: Unrealized PnL calculation
- **WHEN** position entry_price=200, size_usd=1000, current_price=220
- **THEN** unrealized_pnl_usd=100, unrealized_pnl_pct=0.10

#### Scenario: No open positions
- **WHEN** all positions are closed
- **THEN** get_positions returns empty list

### Requirement: get_position returns single asset position
The system SHALL provide `PortfolioService.get_position(asset, current_price)` returning a single `PositionView` for the open position on that asset, or None if no open position exists.

#### Scenario: Position found
- **WHEN** an open position for "ETH" exists
- **THEN** get_position("ETH", current_price) returns that position's view

#### Scenario: No position for asset
- **WHEN** no open position for "BTC" exists
- **THEN** get_position("BTC", ...) returns None

### Requirement: get_daily_pnl returns today's realized PnL
The system SHALL provide `PortfolioService.get_daily_pnl()` returning the sum of `realized_pnl_usd` for all positions closed on or after UTC midnight today.

#### Scenario: Daily PnL sums closed positions
- **WHEN** two positions closed today with realized_pnl_usd=50 and -20
- **THEN** get_daily_pnl returns 30

#### Scenario: Yesterday's positions excluded
- **WHEN** a position was closed yesterday (before UTC midnight)
- **THEN** get_daily_pnl does not include it

### Requirement: Write operations protected by asyncio.Lock
The system SHALL protect `record_open` and `record_close` with an asyncio.Lock to prevent concurrent writes from corrupting portfolio state.

#### Scenario: Concurrent record_open calls
- **WHEN** two coroutines call record_open concurrently
- **THEN** both complete without error and both positions are persisted

### Requirement: record_open atomically persists a new position after execution
`PortfolioService.record_open(intent, result, max_positions)` SHALL, under the write lock:
1. Check open position count against `max_positions` — return `False` if at limit (no DB writes).
2. Read current balance state to compute `size_usd` if `result.size_usd` is `None` (live engine path).
3. Build and insert a Position row (status="open"), a Trade row (direction="open"), and a PortfolioSnapshot row atomically.
4. Return `True` if the position was opened.

#### Scenario: Position opened below limit
- **WHEN** open_count < max_positions and record_open is called
- **THEN** returns True, Position and Trade rows are persisted, PortfolioSnapshot is written

#### Scenario: Position rejected at limit
- **WHEN** open_count >= max_positions and record_open is called
- **THEN** returns False, no DB rows written

#### Scenario: size_usd derived from balance when result.size_usd is None
- **WHEN** result.size_usd is None (live engine) and stablecoin_balance=5000, intent.size_pct=0.2
- **THEN** the Position row is written with size_usd=1000.0

### Requirement: record_close atomically closes a position after execution
`PortfolioService.record_close(asset, result)` SHALL, under the write lock:
1. Look up the open position for `asset`.
2. If no open position exists, return without writing (no-op).
3. Compute realized PnL: `(result.executed_price - entry_price) / entry_price × size_usd`.
4. Update Position to status="closed" with exit_price, realized_pnl_usd, realized_pnl_pct, closed_at.
5. Insert a Trade row (direction="close") and a PortfolioSnapshot.

#### Scenario: Position closed and PnL computed
- **WHEN** record_close is called for an open LONG with entry_price=2000, size_usd=1000, executed_price=2200
- **THEN** Position status="closed", realized_pnl_usd=100, realized_pnl_pct=0.10

#### Scenario: record_close with no open position is a no-op
- **WHEN** record_close is called for an asset with no open position
- **THEN** no DB rows written, no error raised

### Requirement: can_open_position pre-check for fast rejection
`PortfolioService.can_open_position(max_positions)` SHALL return `False` if the current open position count is already at or above `max_positions`, without acquiring the write lock. This is a non-atomic fast-path check — `record_open` still enforces the limit atomically.

#### Scenario: At limit returns False
- **WHEN** open_count == max_positions
- **THEN** can_open_position returns False

#### Scenario: Below limit returns True
- **WHEN** open_count < max_positions
- **THEN** can_open_position returns True

### Requirement: DTO types for service return values
The system SHALL define `PortfolioSummary` and `PositionView` dataclasses in `trading_mcp/domain/dto.py`. These SHALL NOT be SQLAlchemy ORM types — they are plain Python dataclasses usable without an active session.

#### Scenario: PositionView is plain dataclass
- **WHEN** get_positions returns a list
- **THEN** each item is a PositionView instance with no SQLAlchemy session dependency
