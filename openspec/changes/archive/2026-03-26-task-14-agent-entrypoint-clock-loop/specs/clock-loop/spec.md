## ADDED Requirements

### Requirement: Clock loop fires every 15 minutes
The clock loop SHALL execute its cycle immediately on first call, then repeat every 15 minutes using `asyncio.sleep`.

#### Scenario: First execution
- **WHEN** the clock loop coroutine starts
- **THEN** a cycle runs immediately without waiting 15 minutes

#### Scenario: Subsequent executions
- **WHEN** a cycle completes
- **THEN** the loop waits 15 minutes before the next cycle

### Requirement: Position cap is checked before signal gather
The clock loop SHALL call `get_portfolio()` once at the start of each cycle to check the position cap. If `open_position_count >= max_concurrent_positions`, the loop SHALL skip the cycle without fetching any further signals.

#### Scenario: Position cap reached early check
- **WHEN** the initial `get_portfolio()` call shows `open_position_count >= max_concurrent_positions`
- **THEN** cycle is skipped immediately, no further MCP calls are made

#### Scenario: Portfolio fetch fails early
- **WHEN** the initial `get_portfolio()` call raises an exception
- **THEN** cycle is skipped and an error is logged

### Requirement: Cooldown gate pre-filters candidate assets before signal gather
After the position cap check passes, the clock loop SHALL compute `allowed_assets = [a for a in config.assets.tracked if cooldown_gate.is_cooldown_open(a)]` synchronously. Prices, onchain, and news signals SHALL only be fetched for `allowed_assets`. Portfolio SHALL be fetched again as part of the main signal gather (internal DB, acceptable cost).

#### Scenario: All assets available
- **WHEN** no asset is under cooldown
- **THEN** all tracked assets are passed to signal gather

#### Scenario: Some assets on cooldown
- **WHEN** one or more assets have active cooldowns
- **THEN** only allowed assets are passed to price/onchain/news clients; portfolio is still fetched

#### Scenario: All assets on cooldown
- **WHEN** every tracked asset has an active cooldown
- **THEN** `allowed_assets` is empty; price/onchain/news clients are called with an empty list; portfolio is still fetched; brain is still called (may return FLAT intents from portfolio)

### Requirement: Signal gather failure skips the cycle
The clock loop SHALL gather signals via `asyncio.gather`. If any client raises an exception, the loop SHALL log an error and return without running the brain or submitting any intent.

#### Scenario: All signals gathered successfully
- **WHEN** all MCP calls succeed
- **THEN** a `SignalBundle` is assembled and passed to brain

#### Scenario: One MCP client raises during gather
- **WHEN** any MCP client raises an exception during signal gather
- **THEN** the cycle is skipped, an error is logged identifying the failure, and no brain call or intent submission occurs


### Requirement: Intents are submitted FLAT-first, then LONG
The clock loop SHALL sort intents so all `action == "FLAT"` intents are submitted before any `action == "LONG"` intents.

#### Scenario: Mixed intent list
- **WHEN** brain returns a mix of FLAT and LONG intents
- **THEN** all FLAT intents are submitted first, in their original relative order; LONG intents follow

### Requirement: Cooldown is recorded after each successful submission
After `execute_swap` returns without raising, the clock loop SHALL call `cooldown_gate.record_trade(asset)` for that asset.

#### Scenario: Successful execution
- **WHEN** `execute_swap` returns successfully
- **THEN** `record_trade(asset)` is called immediately after

#### Scenario: execute_swap raises
- **WHEN** `execute_swap` raises an exception
- **THEN** `record_trade` is NOT called for that asset; the loop continues to the next intent
