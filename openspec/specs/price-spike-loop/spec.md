### Requirement: Spike detection via 1m and 5m price change windows
The loop SHALL poll `price_feed-mcp` every 60 seconds using `get_prices_change` for all tracked assets. An asset is considered spiking if `abs(change_1m) >= threshold OR abs(change_5m) >= threshold`, where threshold comes from `config.brain.price_spike_threshold_pct`.

#### Scenario: Asset spikes on 1m window
- **WHEN** an asset's 1-minute price change meets or exceeds the threshold in absolute value
- **THEN** that asset is flagged as spiking for this poll cycle

#### Scenario: Asset spikes on 5m window only
- **WHEN** an asset's 5-minute price change meets or exceeds the threshold but 1m does not
- **THEN** that asset is still flagged as spiking

#### Scenario: No assets spike
- **WHEN** all assets have 1m and 5m changes below the threshold
- **THEN** no brain cycle is triggered and the loop sleeps until next poll

### Requirement: Per-asset cooldown gate check before brain trigger
For each spiking asset, the loop SHALL check `CooldownGate.is_cooldown_open(asset)` before including it in the trigger set. Assets with closed cooldown gates are silently skipped.

#### Scenario: Spiking asset in cooldown
- **WHEN** an asset spikes AND its cooldown gate is closed
- **THEN** that asset is excluded from the trigger and no brain run fires for it

#### Scenario: Spiking asset not in cooldown
- **WHEN** an asset spikes AND its cooldown gate is open
- **THEN** that asset is included in the trigger set

#### Scenario: All spiking assets are in cooldown
- **WHEN** one or more assets spike but all have closed cooldown gates
- **THEN** no brain cycle is triggered

### Requirement: Full brain cycle fires once per spike event
When at least one spiking, non-gated asset is detected, the loop SHALL trigger the full brain cycle exactly once (not once per spiking asset). The brain cycle is identical to the ClockLoop cycle: gather all signals for all non-gated assets, run all three brain stages, submit intents.

#### Scenario: Single asset spikes and passes gate
- **WHEN** exactly one asset spikes with an open gate
- **THEN** the full brain cycle fires once

#### Scenario: Multiple assets spike simultaneously
- **WHEN** two or more assets spike with open gates
- **THEN** the full brain cycle fires exactly once (not once per spiking asset)

### Requirement: Spike polling uses change-only fetch
The loop SHALL use a lightweight `get_prices_change_only()` call (single `get_prices_change` RPC) for the 60-second spike check, not the full `get_prices()` bundle (which also fetches indicators). Indicators are only needed if the brain actually runs.

#### Scenario: No spike detected — no indicator fetch
- **WHEN** the spike poll finds no spiking assets
- **THEN** only one RPC call is made (`get_prices_change`), not three

#### Scenario: Spike detected — full signal gather happens in brain cycle
- **WHEN** a spike is detected and the brain cycle fires
- **THEN** the brain gathers the full signal bundle (including indicators) as part of its normal execution
