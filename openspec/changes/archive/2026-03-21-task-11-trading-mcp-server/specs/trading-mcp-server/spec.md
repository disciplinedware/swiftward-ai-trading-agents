## ADDED Requirements

### Requirement: Server starts and runs migrations
The trading-mcp server SHALL run Alembic migrations (`upgrade head`) during startup before accepting any requests. If migrations fail, the server SHALL NOT start.

#### Scenario: Clean start applies migrations
- **WHEN** the server starts with a fresh database
- **THEN** all schema migrations are applied and the server begins accepting requests

#### Scenario: Migrations are idempotent on restart
- **WHEN** the server restarts with migrations already at head
- **THEN** Alembic no-ops and the server starts normally

### Requirement: Health endpoint
The server SHALL expose `GET /health` returning `{"status": "ok"}` with HTTP 200.

#### Scenario: Health check returns ok
- **WHEN** a client sends `GET /health`
- **THEN** the server responds with HTTP 200 and body `{"status": "ok"}`

### Requirement: execute_swap tool
The server SHALL expose an `execute_swap` MCP tool that accepts a serialized `TradeIntent` dict, validates it, fetches the current asset price from `price_feed_mcp`, executes via the configured engine (paper or live), and returns an execution result.

#### Scenario: Successful LONG execution in paper mode
- **WHEN** `execute_swap` is called with a valid LONG TradeIntent and the engine is paper
- **THEN** the response contains `status="executed"`, a `tx_hash` starting with `"paper_"`, `executed_price`, and `slippage_pct`

#### Scenario: Rejected execution when at max positions
- **WHEN** `execute_swap` is called with a LONG TradeIntent and open positions are already at the configured maximum
- **THEN** the response contains `status="rejected"` and a non-empty `reason`

#### Scenario: ERC-8004 validation hook fires after LONG
- **WHEN** a LONG trade executes successfully
- **THEN** `submit_validation` is scheduled as a non-blocking async task

#### Scenario: ERC-8004 reputation hook fires after FLAT
- **WHEN** a FLAT trade executes successfully (closing a position)
- **THEN** `submit_reputation` is scheduled as a non-blocking async task

### Requirement: get_portfolio tool
The server SHALL expose a `get_portfolio` MCP tool that accepts a list of tracked asset symbols, fetches their current prices from `price_feed_mcp`, and returns the full portfolio summary including open positions with unrealized PnL.

#### Scenario: Portfolio with no open positions
- **WHEN** `get_portfolio` is called and no positions are open
- **THEN** the response contains `open_position_count=0` and `stablecoin_balance` equal to starting balance

#### Scenario: Portfolio with open positions includes unrealized PnL
- **WHEN** `get_portfolio` is called and positions are open
- **THEN** each position in `open_positions` has non-null `unrealized_pnl_usd` and `current_price`

### Requirement: get_positions tool
The server SHALL expose a `get_positions` MCP tool that returns all currently open positions with unrealized PnL using current prices fetched from `price_feed_mcp`.

#### Scenario: Returns open positions only
- **WHEN** `get_positions` is called after one LONG and one FLAT (closing)
- **THEN** only the remaining open position is returned

### Requirement: get_position tool
The server SHALL expose a `get_position` MCP tool that accepts a single asset symbol and returns the open position for that asset, or null if none exists.

#### Scenario: Returns position for open asset
- **WHEN** `get_position` is called for an asset with an open position
- **THEN** the position view is returned with correct entry price and asset symbol

#### Scenario: Returns null for asset with no open position
- **WHEN** `get_position` is called for an asset with no open position
- **THEN** null is returned

### Requirement: get_daily_pnl tool
The server SHALL expose a `get_daily_pnl` MCP tool that returns the sum of realized PnL for positions closed on the current UTC day.

#### Scenario: Returns zero with no closed positions today
- **WHEN** `get_daily_pnl` is called with no closed positions today
- **THEN** the response is `"0"` (as a Decimal string)

### Requirement: Engine selection by config
The server SHALL route all `execute_swap` calls to `PaperEngine` when `config.trading.mode = "paper"` and to `LiveEngine` when `config.trading.mode = "live"`.

#### Scenario: Paper mode uses PaperEngine
- **WHEN** `config.trading.mode` is `"paper"` and `execute_swap` is called
- **THEN** the tx_hash in the result begins with `"paper_"`

#### Scenario: Live mode uses LiveEngine
- **WHEN** `config.trading.mode` is `"live"` and `execute_swap` is called
- **THEN** `LiveEngine.execute_swap` is invoked (may raise if Risk Router not configured)

### Requirement: ERC-8004 identity registration on startup
The server lifespan SHALL trigger `ERC8004Registry.register_identity()` as a non-blocking task after startup. If an Agent row already exists in the DB, the call MUST be a no-op.

#### Scenario: Identity registered on first startup
- **WHEN** the server starts with no Agent row in the DB
- **THEN** `register_identity()` is called and an Agent row is written

#### Scenario: Identity skipped on subsequent startups
- **WHEN** the server starts and an Agent row already exists
- **THEN** `register_identity()` returns immediately without any on-chain call
