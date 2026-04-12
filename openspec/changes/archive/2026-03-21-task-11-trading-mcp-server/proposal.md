## Why

Tasks 7–10 built all the internals of `trading_mcp` (schema, portfolio state, paper engine, ERC-8004 hooks) but no HTTP server wires them together. Without a running FastAPI app on port 8005, no agent can call `execute_swap`, `get_portfolio`, or the other portfolio tools — making the entire trading layer unreachable.

## What Changes

- New `src/trading_mcp/infra/price_client.py` — async HTTP client that calls `price_feed_mcp` via JSON-RPC to fetch current prices (used by engine and portfolio views)
- New `src/trading_mcp/engine/live.py` — `LiveEngine` skeleton: EIP-712 calldata construction + Risk Router submission via web3.py (raises `NotImplementedError` when Risk Router address is unset; fully activatable on hackathon Day 1)
- New `src/trading_mcp/service/trading_service.py` — orchestration layer: fetches prices, calls engine, fires ERC-8004 async hooks
- New `src/trading_mcp/server.py` — FastMCP app on port 8005; lifespan runs Alembic migrations and wires all deps; exposes `execute_swap`, `get_portfolio`, `get_positions`, `get_position`, `get_daily_pnl` tools plus `/health`
- New `tests/trading_mcp/test_server.py` — integration tests covering full `execute_swap → portfolio state` flow and health check (paper mode + SQLite in-memory)

## Capabilities

### New Capabilities

- `trading-mcp-server`: FastMCP HTTP server on port 8005 that exposes the full trading interface (execute_swap, portfolio reads) over MCP JSON-RPC; startup runs migrations and checks ERC-8004 identity
- `trading-mcp-live-engine`: EIP-712 calldata construction and Risk Router contract submission via web3.py with 60 s confirmation timeout
- `trading-mcp-price-client`: Internal HTTP client for fetching current asset prices from `price_feed_mcp` (used for paper fill price and portfolio unrealized PnL)

### Modified Capabilities

## Impact

- New file: `src/trading_mcp/infra/price_client.py`
- New file: `src/trading_mcp/engine/live.py`
- New file: `src/trading_mcp/service/__init__.py` + `src/trading_mcp/service/trading_service.py`
- New file: `src/trading_mcp/server.py`
- New test file: `tests/trading_mcp/test_server.py`
- No changes to existing modules (engine/paper.py, domain/*, erc8004/*, infra/db.py)
- No new dependencies required (web3, aiohttp, httpx, fastmcp, alembic all already in pyproject.toml)
