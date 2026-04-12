## Why

Open positions are protected by ATR-based stop-loss and take-profit levels set at entry, but nothing enforces them between clock cycles. Without a dedicated watchdog, a position could breach its stop-loss or hit its target mid-cycle and stay open for up to 15 minutes, turning a controlled loss or win into a worse outcome.

## What Changes

- Add `src/agent/infra/price_feed_mcp.py::get_prices_latest()` — slim MCP call returning only current prices (no indicators/changes) for use in tight polling loops
- Add `src/agent/loops/stop_loss.py` — new `StopLossLoop` class polling every 2 minutes; checks each open position's price against its stop_loss and take_profit levels; fires a FLAT intent immediately on breach; bypasses the cooldown gate check but records the cooldown after exit
- Wire `StopLossLoop` into `src/agent/main.py`, replacing the `_noop_loop()` placeholder marked Task 18
- Add `tests/agent/loops/test_stop_loss.py` covering breach detection (both directions), no-LLM path, and concurrent price fetches

## Capabilities

### New Capabilities

- `stop-loss-loop`: Continuous 2-minute watchdog that fires FLAT intents when open position prices breach their stop_loss or take_profit levels, bypassing the cooldown gate check while still recording cooldown after exit

### Modified Capabilities

- `mcp-clients`: Adding `get_prices_latest()` to `PriceFeedMCPClient` — new method on existing client, no requirement changes to the interface contract

## Impact

- New file: `src/agent/loops/stop_loss.py`
- Modified: `src/agent/infra/price_feed_mcp.py` (new method)
- Modified: `src/agent/main.py` (loop wired in)
- New test file: `tests/agent/loops/test_stop_loss.py`
- No new dependencies; uses existing `httpx`, `asyncio`, `Decimal` primitives
