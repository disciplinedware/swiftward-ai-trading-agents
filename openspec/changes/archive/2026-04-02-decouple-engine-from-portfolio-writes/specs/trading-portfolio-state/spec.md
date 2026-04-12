## ADDED Requirements

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
