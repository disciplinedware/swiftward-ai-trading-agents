## Why

The agent's 15-minute clock loop is too slow to react to sudden price spikes. A ±3–5% move in 1–5 minutes represents a high-conviction entry or exit signal that should trigger an immediate brain evaluation, not wait up to 15 minutes for the next clock tick.

## What Changes

- Add `src/agent/loops/price_spike.py` — new `PriceSpikeLoop` class that polls price changes every 60 seconds
- Detect spikes using existing `get_prices_change` data (1m and 5m windows) from price_feed-mcp
- On spike detection: check per-asset cooldown gate, then fire the full brain cycle for spiking assets
- Wire `PriceSpikeLoop` into `main.py`, replacing the `_noop_loop()` placeholder at Task 19's slot
- Add `get_prices_change_only()` method to `PriceFeedMCPClient` to avoid fetching full indicator bundle during spike polling

## Capabilities

### New Capabilities
- `price-spike-loop`: A 60-second polling loop that detects ±threshold% price moves and triggers the full brain cycle for affected assets, gated by per-asset cooldown

### Modified Capabilities

## Impact

- **New file**: `src/agent/loops/price_spike.py`
- **Modified**: `src/agent/infra/price_feed_mcp.py` — add `get_prices_change_only()` method
- **Modified**: `src/agent/main.py` — replace `_noop_loop()` Task 19 placeholder with `PriceSpikeLoop`
- **Modified**: `tests/agent/` — new test file for spike detection and gate suppression
- **No new dependencies** — uses existing `PriceFeedMCPClient`, `CooldownGate`, `ClockLoop`
