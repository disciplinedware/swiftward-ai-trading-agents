## ADDED Requirements

### Requirement: Stop-loss / take-profit watchdog loop
The system SHALL continuously monitor all open positions and fire immediate FLAT intents when the current market price breaches a position's stop_loss or take_profit level.

#### Scenario: Stop-loss breach detected
- **WHEN** the current price of an open position's asset is less than or equal to the position's `stop_loss` level
- **THEN** the loop SHALL immediately submit a FLAT intent to trading-mcp with `trigger_reason="stop_loss"` and `reasoning` that includes the current price and stop_loss level

#### Scenario: Take-profit breach detected
- **WHEN** the current price of an open position's asset is greater than or equal to the position's `take_profit` level
- **THEN** the loop SHALL immediately submit a FLAT intent to trading-mcp with `trigger_reason="take_profit"` and `reasoning` that includes the current price and take_profit level

#### Scenario: No breach
- **WHEN** the current price is strictly between stop_loss and take_profit for all open positions
- **THEN** the loop SHALL take no action and wait for the next 2-minute poll

### Requirement: Loop polls every 2 minutes
The loop SHALL poll trading-mcp for open positions and price_feed-mcp for current prices on a fixed 2-minute interval.

#### Scenario: Steady-state polling
- **WHEN** the loop is running and no breach occurs
- **THEN** it SHALL sleep 120 seconds between each check cycle

### Requirement: Bypasses cooldown gate check
The loop SHALL NOT call `gate.is_allowed()` or `gate.is_cooldown_open()` before firing a FLAT intent. Stop-loss / take-profit exits are always permitted regardless of cooldown state.

#### Scenario: Asset in cooldown receives a stop-loss exit
- **WHEN** an asset's cooldown gate is closed (within 30-minute window)
- **THEN** the stop-loss loop SHALL still fire a FLAT intent for that asset

### Requirement: Records cooldown after exit
After successfully submitting a FLAT intent for a breached position, the loop SHALL call `gate.record_trade(asset)` to close the cooldown gate for that asset.

#### Scenario: Cooldown recorded post-exit
- **WHEN** a FLAT intent is successfully submitted for an asset
- **THEN** `gate.record_trade(asset)` SHALL be called so the clock loop does not re-enter that asset within the cooldown window

### Requirement: Concurrent price fetches
The loop SHALL fetch current prices for all open positions concurrently using a single batched call, not sequentially per asset.

#### Scenario: Multiple open positions
- **WHEN** there are N open positions
- **THEN** the loop SHALL fetch all N prices in a single `get_prices_latest(assets)` call

### Requirement: No LLM invocation
The stop-loss loop SHALL make no LLM calls. The breach decision is purely deterministic (price comparison against stored levels).

#### Scenario: Breach detected
- **WHEN** a price breach is detected
- **THEN** the FLAT intent SHALL be constructed and submitted without any LLM call

### Requirement: Error resilience
If trading-mcp or price_feed-mcp returns an error during a poll cycle, the loop SHALL log the error and skip the cycle, retrying at the next 2-minute interval.

#### Scenario: MCP error during cycle
- **WHEN** `get_portfolio()` or `get_prices_latest()` raises an exception
- **THEN** the loop SHALL log the error and continue to the next cycle without crashing
