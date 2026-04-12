## Context

The agent runs four loops concurrently (clock 15m, price spike 1m, stop-loss 2m, tier2 5m). All four share a single `CooldownGate` instance. Without this gate, two loops could simultaneously decide to trade the same asset, or the agent could exceed the configured maximum concurrent positions.

The gate has two distinct concerns:
1. **Per-asset timer** — was a trade recorded on this asset in the last N minutes?
2. **Global position count** — are we already at max concurrent positions?

The stop-loss loop is exempt from the gate entirely (it calls trading-mcp directly without going through `is_allowed`). That exemption is the loop's responsibility, not the gate's.

At this stage, `MCPClient` (Task 14) does not yet exist. The gate depends on it only for `global_positions_ok()`. Tests will mock it.

## Goals / Non-Goals

**Goals:**
- Single `is_allowed(asset)` call that checks both concerns atomically (under a lock)
- `record_trade(asset)` records wall-clock timestamp for the per-asset timer
- Live position count sourced from trading-mcp (no drift on restart)
- Async-safe under concurrent loop access (`asyncio.Lock`)
- Fully testable without a running MCP server

**Non-Goals:**
- Tracking position opens/closes internally (would drift on restart; trading-mcp is the source of truth)
- Rate limiting beyond the 30-min cooldown window
- Any stop-loss bypass logic (the loop handles that)

## Decisions

### `is_allowed` combines both checks in one public method

**Decision**: One method `async is_allowed(asset) -> bool` instead of separate `is_open(asset)` and `global_positions_ok()`.

**Rationale**: All four callers need both checks. A single method prevents partial use (e.g., forgetting to call the second check). Internals split into `_is_cooldown_open(asset)` (sync dict lookup) and `_global_positions_ok()` (async MCP call). Cooldown checked first — if already suppressed, no MCP call is made.

### Position count sourced from trading-mcp

**Decision**: `_global_positions_ok()` calls `mcp_client.get_portfolio()` and reads `open_position_count`.

**Alternatives considered**:
- *Internal counter* — increment on `record_trade`, decrement on close. Rejected: drifts on restart, requires a `record_close()` that complicates callers.
- *Portfolio snapshot passed as argument* — `is_allowed(asset, portfolio)`. Rejected: forces callers to fetch portfolio before calling the gate, coupling them unnecessarily.

### Wall-clock time via `datetime.now(timezone.utc)`

**Decision**: Use `datetime.now(timezone.utc)` for timestamps. Tests use `time_machine` to freeze time.

**Alternatives considered**:
- *Injected time provider* — cleaner but adds indirection for a simple case. `time_machine` patches at the stdlib level, so no custom abstraction needed.
- *`asyncio.get_event_loop().time()`* — monotonic, but not timezone-aware and harder to freeze in tests.

### `asyncio.Lock` for shared state

**Decision**: A single `asyncio.Lock` guards both `_timestamps` dict reads/writes and the MCP call in `is_allowed`.

**Rationale**: Prevents two loops from both passing the cooldown check before either calls `record_trade`. The lock is held for the duration of both checks. Since MCP calls are fast (local network), this is acceptable.

## Risks / Trade-offs

- **MCP call inside lock** — If trading-mcp is slow or unavailable, `is_allowed` blocks all loops. Mitigation: MCP health check at startup (Task 14); gate returns `False` (suppress) on MCP error rather than raising, with a log warning.
- **`record_trade` is not awaited** — It's sync (just writes to dict). If a loop forgets to call it after a successful trade, the gate stays open. Mitigation: well-documented contract; Task 14 wires this correctly.
- **No persistence** — Cooldown state lives in memory. On agent restart, all gates reset to open. Acceptable: the 30-min window is short enough that a restart is unlikely to cause a double trade on the same asset.

## Migration Plan

No migration needed. This is a new module with no existing callers until Task 14 wires the loops.
