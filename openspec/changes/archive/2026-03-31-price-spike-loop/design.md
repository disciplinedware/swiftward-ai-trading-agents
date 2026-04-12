## Context

The agent has four concurrent async loops. Task 18 (ExitWatchdog) and Task 14 (ClockLoop) are implemented. Tasks 19 and 20 have `_noop_loop()` placeholders in `main.py`. The ClockLoop already encapsulates the full brain-run pipeline in `_run_once()`. `PriceFeedMCPClient` exposes `get_prices()` (full bundle: latest + changes + indicators) and `get_prices_latest()`, but not a lightweight change-only fetch.

## Goals / Non-Goals

**Goals:**
- Poll all tracked assets for 1m/5m price changes every 60 seconds
- Detect ±`config.brain.price_spike_threshold_pct`% moves (absolute value check on either window)
- Per-asset cooldown gate check before triggering brain
- Trigger the full brain cycle (same as ClockLoop) for any spiking, non-gated asset
- Handle multi-asset simultaneous spikes correctly

**Non-Goals:**
- No separate "spike-scoped" brain run — always full brain with all allowed assets
- No new config keys (reuses `brain.price_spike_threshold_pct`)
- No persistent spike state between polls

## Decisions

### Decision 1: PriceSpikeLoop triggers ClockLoop._run_once() directly

**Chosen**: `PriceSpikeLoop` holds a reference to `ClockLoop` and calls `clock._run_once()` when a spike is detected.

**Alternatives considered**:
- *Duplicate brain-run logic*: Violates DRY, maintenance burden, risk of the two diverging silently.
- *Extract shared `run_brain_cycle()` helper*: Clean but adds abstraction for just two callers. Since `ClockLoop._run_once()` already does exactly this, referencing it is simpler.

**Rationale**: Both loops are wired together in `main.py` and share the same `CooldownGate` instance. The coupling is intentional — spike is just an alternative trigger for the same brain cycle.

### Decision 2: Add `get_prices_change_only()` to PriceFeedMCPClient

**Chosen**: Add a lightweight `get_prices_change_only(assets) -> dict[str, dict[str, Decimal]]` method that calls only `get_prices_change` (one RPC call vs. three in `get_prices()`).

**Rationale**: The spike poll runs every 60s for all 10 assets. Fetching full indicator bundles (RSI, EMA, ATR, BB) every 60s is wasted work — indicators are only needed if the brain actually runs. The change-only call is ~3× cheaper.

### Decision 3: Spike fires brain if ANY tracked asset breaches threshold

**Chosen**: Collect all spiking assets that pass the cooldown gate, then call `clock._run_once()` once (not once per spiking asset). The brain handles asset selection internally.

**Rationale**: The brain's Stage 2 (rotation selector) already scores and selects the best 1–2 assets from all tracked assets. Firing the brain once is correct — it will naturally prioritize the spiking assets if they score well.

### Decision 4: Check both 1m and 5m windows independently

**Chosen**: A spike is detected if `abs(change_1m) >= threshold OR abs(change_5m) >= threshold` for any asset.

**Rationale**: A 5m move catches sustained momentum that 1m might miss if the move started 2–3 minutes ago. Both windows are already returned by `get_prices_change`.

## Risks / Trade-offs

- **Spike during active brain run**: If ClockLoop is mid-run when spike fires `_run_once()`, two brain runs execute concurrently. The `CooldownGate` provides per-asset protection — the second run will find the gate closed for any asset that got a trade in the first run. Acceptable for a hackathon; a production system would add a brain-run mutex.
- **False spike suppression**: If cooldown gate is closed for the spiking asset (e.g., just traded it), the spike is silently suppressed. This is correct behavior per spec.
- **60s polling adds minor load**: 10 assets × 1 RPC call every 60s. Binance rate limits are generous; no concern.

## Migration Plan

1. Add `get_prices_change_only()` to `PriceFeedMCPClient`
2. Implement `PriceSpikeLoop` in `src/agent/loops/price_spike.py`
3. Replace `_noop_loop()` Task 19 placeholder in `main.py`
4. Update `progress.md` to mark Task 19 done
