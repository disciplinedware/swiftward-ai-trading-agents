## Why

The agent runs four concurrent trigger loops that can all fire on the same asset simultaneously. Without a gate, the agent can submit multiple trades for the same asset within seconds, and can open more positions than the configured maximum. Task 14 (agent entrypoint + clock loop) depends on this gate being available before wiring the loops together.

## What Changes

- New `CooldownGate` class at `src/agent/trigger/cooldown.py`
- Two public methods: `is_allowed(asset)` checks per-asset cooldown + global position count; `record_trade(asset)` closes the gate for the cooldown window
- Per-asset cooldown duration read from `config.trading.cooldown_minutes`
- Max concurrent positions read from `config.trading.max_concurrent_positions`; live count fetched from trading-mcp on each `is_allowed` call
- `src/agent/trigger/__init__.py` created to make the package importable
- Tests at `tests/agent/trigger/test_cooldown.py`

## Capabilities

### New Capabilities

- `cooldown-gate`: Per-asset async gate that suppresses redundant trades. Combines a wall-clock per-asset cooldown timer with a live position count check against trading-mcp. Shared across all four agent loops.

### Modified Capabilities

(none)

## Impact

- New file: `src/agent/trigger/cooldown.py`
- New file: `src/agent/trigger/__init__.py`
- New file: `tests/agent/trigger/__init__.py`
- New file: `tests/agent/trigger/test_cooldown.py`
- Consumed by Task 14 (`src/agent/loops/clock.py` and other loops)
- Depends on `MCPClient` (Task 14) — for testing, the MCP client is mocked
