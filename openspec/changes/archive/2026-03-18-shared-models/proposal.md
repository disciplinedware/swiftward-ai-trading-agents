## Why

All downstream components — the agent brain, MCP servers, and backtesting runner — need a shared contract for the data shapes that flow between them (`TradeIntent`, `Position`, `SignalBundle`). Without these defined once and imported everywhere, each component would define its own version and diverge. Task 2 establishes this shared contract before any data-consuming component is built.

## What Changes

- Add `src/common/models/` package with three model files
- Add `src/common/models/__init__.py` re-exporting all public types
- Update `python/CLAUDE.md` to reflect `src/common/models/` (not `src/agent/models/`) and note Pydantic usage
- Add tests for round-trip serialization and Literal field validation

## Capabilities

### New Capabilities

- `trade-intent`: `TradeIntent` Pydantic model — the shape sent from agent brain to trading-mcp (via SwiftWard). Fields: asset, action (LONG|FLAT), size_pct, stop_loss, take_profit, strategy, reasoning_uri, trigger_reason. Financial fields as `Decimal`, serialized to `str`.
- `position`: `Position` Pydantic model — open or closed position tracked by the stop-loss loop and portfolio state. Single class with `Optional` fields for closed-position data (exit_price, realized_pnl_usd, etc.).
- `signal-bundle`: `SignalBundle` Pydantic model — aggregated input to the brain from all MCP servers. One field per MCP (`prices`, `fear_greed`, `onchain`, `news`). Sub-classes per MCP as stubs; structure finalized during MCP implementation tasks.

### Modified Capabilities

## Impact

- `src/common/models/` — new package, 4 new files
- `python/CLAUDE.md` — path correction (`src/agent/models/` → `src/common/models/`) + Pydantic note
- `tests/common/test_models.py` — new test file
- All future tasks (3–24) import from `common.models` — interface must not change lightly after this point
