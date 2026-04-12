## ADDED Requirements

### Requirement: LiveEngine produces DB records via TradingService after confirmation
After `LiveEngine.execute_swap` returns a successful `ExecutionResult`, `TradingService` SHALL call `PortfolioService.record_open` or `record_close` to persist the trade to the database. The LiveEngine itself remains DB-free.

#### Scenario: Portfolio readable after live LONG
- **WHEN** a LONG executes successfully via LiveEngine
- **THEN** `get_portfolio` and `get_positions` return the new position

#### Scenario: Portfolio readable after live FLAT
- **WHEN** a FLAT executes successfully via LiveEngine
- **THEN** the closed position appears with realized PnL in `get_daily_pnl`
