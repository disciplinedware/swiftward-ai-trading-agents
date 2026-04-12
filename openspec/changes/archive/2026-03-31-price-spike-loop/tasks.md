## 1. Extend PriceFeedMCPClient

- [x] 1.1 Add `get_prices_change_only(assets: list[str]) -> dict[str, dict[str, Decimal]]` to `src/agent/infra/price_feed_mcp.py` — single `get_prices_change` RPC call, returns change values keyed by asset then window name

## 2. Implement PriceSpikeLoop

- [x] 2.1 Create `src/agent/loops/price_spike.py` with `PriceSpikeLoop` class; constructor accepts `price_feed: PriceFeedMCPClient`, `gate: CooldownGate`, `clock: ClockLoop`, `config: AgentConfig`
- [x] 2.2 Implement `run()` — infinite loop: call `_check()`, then `asyncio.sleep(60)`
- [x] 2.3 Implement `_check()` — fetch change data via `get_prices_change_only()` for all tracked assets, detect spiking assets (abs(1m or 5m) >= threshold), filter by cooldown gate, call `clock._run_once()` if any pass
- [x] 2.4 Add structured logging: debug on each poll cycle, info on spike detection with asset/change values, info when brain triggered

## 3. Wire into main.py

- [x] 3.1 Import `PriceSpikeLoop` in `src/agent/main.py`
- [x] 3.2 Instantiate `PriceSpikeLoop` after `ClockLoop` is built (it needs a ref to clock)
- [x] 3.3 Replace the Task 19 `_noop_loop()` with `price_spike.run()` in `asyncio.gather()`
- [x] 3.4 Remove `_noop_loop()` entirely if Tasks 20 is still pending (keep it for slot 20 only)

## 4. Tests

- [x] 4.1 Create `tests/agent/test_price_spike.py` with parametrized test covering: spike on 1m only, spike on 5m only, spike on both windows, no spike (below threshold)
- [x] 4.2 Test cooldown gate suppression: spiking asset with closed gate → brain not triggered
- [x] 4.3 Test multi-asset simultaneous spike: brain fires exactly once regardless of spike count
- [x] 4.4 Test all spiking assets gated: brain not triggered
- [x] 4.5 Test `get_prices_change_only()` in isolation: verify single RPC call made, correct Decimal parsing

## 5. Progress tracking

- [x] 5.1 Update `python/docs/progress.md` — mark Task 19 as `done`
