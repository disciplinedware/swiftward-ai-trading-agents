## Why

The agent has three trigger loops running (clock 15min, exit watchdog 2min, price spike 1min) but the Tier 2 event-driven loop is missing. Without it, the agent cannot react to sudden macro news events or extreme Fear & Greed readings within the 15-minute clock window — leaving money on the table or holding positions through a market panic.

## What Changes

- Add `src/agent/trigger/tier2.py` — `Tier2Loop` class, polls every 5 minutes
- Wire `Tier2Loop` into `src/agent/main.py`, replacing the `_noop_loop()` placeholder
- Two trigger conditions checked each cycle:
  - **Fear & Greed threshold crossing** — index < 20 (extreme fear) or > 80 (extreme greed)
  - **Macro news flag** — high-impact event detected by news-mcp (Fed, ETF, hack, exchange collapse)
- Any condition met → fires full brain cycle (`clock._run_once()`) once per 5-min cycle
- Cooldown gate enforced inside `clock._run_once()` (no extra logic needed)
- Liquidation spike check **omitted** — Binance decommissioned the required endpoint; data is unavailable

## Capabilities

### New Capabilities
- `tier2-loop`: 5-minute event-driven trigger loop reacting to macro news flags and extreme Fear & Greed readings

### Modified Capabilities

## Impact

- New file: `python/src/agent/trigger/tier2.py`
- Modified: `python/src/agent/main.py` — replaces `_noop_loop()` with `Tier2Loop`
- New test file: `python/tests/agent/trigger/test_tier2.py`
- No changes to MCP clients, brain, or config
