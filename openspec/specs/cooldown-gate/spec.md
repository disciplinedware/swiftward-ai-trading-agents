## ADDED Requirements

### Requirement: Per-asset cooldown suppression
The gate SHALL suppress trade attempts on an asset if a trade was recorded on that asset within `config.trading.cooldown_minutes` minutes. Suppression means `is_allowed` returns `False`.

#### Scenario: Asset within cooldown window is suppressed
- **WHEN** `record_trade("BTC")` was called less than `cooldown_minutes` minutes ago
- **THEN** `is_allowed("BTC")` returns `False`

#### Scenario: Asset past cooldown window is allowed
- **WHEN** `record_trade("BTC")` was called more than `cooldown_minutes` minutes ago
- **THEN** `is_allowed("BTC")` returns `True` (assuming position count is below max)

#### Scenario: Asset with no trade history is allowed
- **WHEN** `record_trade` has never been called for an asset
- **THEN** `is_allowed(asset)` returns `True` (assuming position count is below max)

#### Scenario: Per-asset isolation
- **WHEN** `record_trade("BTC")` is called but not `record_trade("ETH")`
- **THEN** `is_allowed("BTC")` returns `False` and `is_allowed("ETH")` returns `True`

### Requirement: Global position count check
The gate SHALL suppress trade attempts when the number of open positions reported by trading-mcp is greater than or equal to `config.trading.max_concurrent_positions`.

#### Scenario: At maximum positions is suppressed
- **WHEN** trading-mcp reports `open_position_count >= max_concurrent_positions`
- **THEN** `is_allowed(asset)` returns `False`

#### Scenario: Below maximum positions is allowed
- **WHEN** trading-mcp reports `open_position_count < max_concurrent_positions`
- **THEN** `is_allowed(asset)` proceeds to the per-asset cooldown check

### Requirement: Cooldown check before position count check
The gate SHALL check the per-asset cooldown first. If the asset is suppressed by cooldown, the gate SHALL NOT call trading-mcp for the position count.

#### Scenario: MCP not called when asset is in cooldown
- **WHEN** the asset is within its cooldown window
- **THEN** `is_allowed` returns `False` without calling `mcp_client.get_portfolio()`

### Requirement: Concurrent access safety
The gate SHALL be safe to call concurrently from multiple asyncio coroutines. Concurrent calls MUST NOT produce race conditions where two callers both pass the gate for the same suppressed asset.

#### Scenario: Concurrent calls on same asset are serialized
- **WHEN** two coroutines call `is_allowed("BTC")` simultaneously with no prior `record_trade`
- **THEN** both calls complete without raising an exception

### Requirement: MCP error handling
If trading-mcp is unreachable during the position count check, the gate SHALL return `False` (suppress the trade) and log a warning. It MUST NOT raise an exception to the caller.

#### Scenario: MCP unavailable suppresses trade
- **WHEN** `mcp_client.get_portfolio()` raises an exception
- **THEN** `is_allowed` returns `False` and logs a warning

### Requirement: record_trade closes the gate
Calling `record_trade(asset)` SHALL record the current wall-clock time for that asset. Subsequent calls to `is_allowed(asset)` within `cooldown_minutes` MUST return `False`.

#### Scenario: Gate closes immediately after record_trade
- **WHEN** `record_trade("ETH")` is called
- **THEN** `is_allowed("ETH")` returns `False` immediately after
