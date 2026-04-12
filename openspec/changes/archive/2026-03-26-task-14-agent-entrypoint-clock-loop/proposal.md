## Why

Tasks 1–13 built all the pieces (MCP servers, brain, cooldown gate) but nothing ties them together into a running process. Task 14 wires everything into a working agent: a startup sequence that validates the environment and a 15-minute clock loop that drives the full signal → brain → execute cycle.

## What Changes

- New `src/agent/infra/` package with five separate MCP client classes (one per server), replacing the single `src/agent/mcp_client.py`
- New `src/agent/main.py` — startup sequence: config load, logger init, parallel MCP health checks, and `asyncio.gather` of all trigger loops
- New `src/agent/trigger/clock.py` — 15-minute clock loop: parallel signal gather, asset pre-filtering by cooldown gate, brain run, FLAT-first submission
- **BREAKING**: `src/agent/mcp_client.py` deleted; `TradingMCPClient` moves to `src/agent/infra/trading_mcp.py`; all existing imports updated

## Capabilities

### New Capabilities

- `agent-startup`: Process startup sequence — config, logger, MCP health checks, loop orchestration
- `clock-loop`: 15-minute trigger loop — signal gather, cooldown pre-filter, brain run, intent submission
- `mcp-clients`: Five typed MCP client classes (trading, price-feed, fear-greed, onchain, news) in `src/agent/infra/`

### Modified Capabilities

- none

## Impact

- `src/agent/trigger/cooldown.py` — import path for `TradingMCPClient` changes
- `tests/agent/trigger/test_cooldown.py` — import updated
- New test files: `tests/agent/infra/test_*.py`, `tests/agent/trigger/test_clock.py`
- No changes to MCP servers, brain, or models
