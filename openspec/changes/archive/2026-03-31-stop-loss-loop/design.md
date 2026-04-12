## Context

The agent runs four async loops concurrently. Tasks 15–17 completed the brain and clock loop (15-min cycle). Tasks 18–20 add the three event-driven trigger loops. The stop-loss / take-profit loop is the highest-priority trigger — it must fire regardless of cooldown gate state and without any LLM call.

Open positions carry `stop_loss` and `take_profit` price levels recorded at entry (ATR-based). Currently nothing monitors these between clock cycles. The `PortfolioSnapshot` model already returns `open_positions: list[OpenPositionView]` with all necessary fields. The `PriceFeedMCPClient` can fetch prices but currently only via a fat `get_prices()` call that also pulls indicators and changes.

`main.py` has a `_noop_loop()` placeholder explicitly for Task 18.

## Goals / Non-Goals

**Goals:**
- Poll open positions every 2 minutes and fire a FLAT intent when price breaches stop_loss or take_profit
- Bypass cooldown gate *check* (no `is_allowed()` call) but still call `gate.record_trade()` after exit so the clock loop doesn't immediately re-enter
- No LLM; purely deterministic price comparison
- Concurrent price fetches across all open positions in a single gather call

**Non-Goals:**
- Partial position closes (not in the `TradeIntent` model)
- Trailing stops or dynamic level adjustment
- Any interaction with the brain pipeline

## Decisions

### D1: Slim `get_prices_latest()` vs reusing `get_prices()`

The existing `get_prices()` makes 3 parallel HTTP calls (latest + changes + indicators). For the stop-loss watchdog, only the raw price is needed.

**Decision**: Add `get_prices_latest(assets: list[str]) -> dict[str, Decimal]` to `PriceFeedMCPClient`. Single JSON-RPC call, returns `Decimal` values directly.

**Rationale**: The stop-loss loop runs every 2 min with up to 5 assets. Calling 3 endpoints when 1 suffices wastes 2 HTTP round-trips per cycle. The slim method also has a cleaner type signature for this use case.

### D2: Cooldown gate — bypass check, record exit

**Decision**: `StopLossLoop` does NOT call `gate.is_allowed()` or `gate.is_cooldown_open()`. It calls `gate.record_trade(asset)` after a successful FLAT execution.

**Rationale**: The requirements explicitly state stop-loss bypasses the cooldown gate. But recording the exit prevents the clock loop from immediately opening a new position in the just-exited asset within the 30-min window — which is the intended behavior.

### D3: `trigger_reason` and `strategy` on FLAT intents

**Decision**: Use `trigger_reason="stop_loss"` for stop-loss exits and `trigger_reason="take_profit"` for take-profit exits. Add `"take_profit"` to the `TriggerReason` literal in `src/common/models/trade_intent.py`. Inherit `strategy` from the position's recorded tag.

**Rationale**: `Position.exit_reason` already distinguishes `"take_profit"` from `"stop_loss"` — the shared model already has the concept. Using `"stop_loss"` for both would lose information in the reasoning trace and make logs ambiguous. The `TriggerReason` addition is a one-line change to a `Literal` type with no other impact.

### D4: File location — `loops/` subdirectory

**Decision**: Create `src/agent/loops/stop_loss.py`. This matches the existing `trigger/clock.py` pattern (which will eventually move to `loops/` per the plan structure) and anticipates Tasks 19–20 adding `price_spike.py` and `tier2.py` alongside it.

## Risks / Trade-offs

- **Stale position data**: If trading-mcp is slow or errored, `get_portfolio()` may return stale positions. Mitigation: log and skip the cycle on error; do not fire exits on stale data.
- **Double FLAT**: If two loop cycles fire before the first FLAT is confirmed, a second FLAT for the same asset may be sent. Mitigation: `execute_swap` on a closed/flat position is idempotent at the trading-mcp level (returns no-op). No in-process dedup is needed.
- **Price fetch failure for one asset**: If `get_prices_latest` errors for a subset of assets, the entire cycle errors. Mitigation: catch per-asset errors or let the full cycle error and retry in 2 min — the 2-min retry is acceptable for this scenario.
