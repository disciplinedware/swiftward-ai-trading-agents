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
The system SHALL protect `open_position` and `close_position` with an asyncio.Lock to prevent concurrent writes from corrupting portfolio state.

#### Scenario: Concurrent open_position calls
- **WHEN** two coroutines call open_position concurrently
- **THEN** both complete without error and both positions are persisted

### Requirement: DTO types for service return values
The system SHALL define `PortfolioSummary` and `PositionView` dataclasses in `trading_mcp/domain/dto.py`. These SHALL NOT be SQLAlchemy ORM types — they are plain Python dataclasses usable without an active session.

#### Scenario: PositionView is plain dataclass
- **WHEN** get_positions returns a list
- **THEN** each item is a PositionView instance with no SQLAlchemy session dependency
