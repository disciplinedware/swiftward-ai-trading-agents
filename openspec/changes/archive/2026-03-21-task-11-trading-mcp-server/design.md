## Context

Tasks 7–10 built `trading_mcp` internals: ORM entities, migrations, `PortfolioService`, `PaperEngine`, and `ERC8004Registry`. All these components are tested in isolation but have no entry point — there is no FastAPI/FastMCP server, no HTTP routes, and no wiring code.

The server must:
1. Run Alembic migrations at startup (safe to re-run; idempotent)
2. Wire all dependencies via lifespan (engine, portfolio service, price client, ERC-8004 registry)
3. Expose 5 MCP tools + `/health` route
4. Route to paper or live engine based on `config.trading.mode`
5. Fire ERC-8004 hooks asynchronously after every executed trade

The existing `PaperEngine.execute_swap(intent, current_price)` takes a pre-fetched price — the server layer must fetch it before calling the engine.

## Goals / Non-Goals

**Goals:**
- Working FastMCP server on port 8005 with all 5 tools
- Migrations run on every startup (alembic upgrade head)
- Paper mode fully functional; live engine skeleton present (raises clearly if unconfigured)
- ERC-8004 hooks fire as non-blocking async tasks after execution
- Full integration test: execute_swap → portfolio state (paper + SQLite)
- Health endpoint returns `{"status": "ok"}`

**Non-Goals:**
- Live engine fully operational (Risk Router ABI/address unknown until hackathon Day 1)
- SwiftWard integration (that's Task 24)
- Agent-side cooldown gate (Task 13)
- Price caching (price_client calls price_feed_mcp every time; no extra cache layer)

## Decisions

### D1 — TradingService as orchestration layer

**Decision**: Introduce `TradingService` between `server.py` tools and the engine/portfolio service.

**Rationale**: The server must do three things per `execute_swap` call — fetch current price, call engine, fire ERC-8004 tasks. Putting this logic in the server's tool handler violates the architecture rule (`server.py` is presentation only). A thin `TradingService` class keeps handlers as one-liners and makes the orchestration testable without FastMCP.

**Alternative considered**: Put logic directly in tool handlers. Rejected — harder to test, violates the established MCP server pattern.

### D2 — PriceClient as infra layer

**Decision**: `trading_mcp/infra/price_client.py` — a thin async HTTP client that calls `price_feed_mcp`'s `get_prices_latest` tool via MCP JSON-RPC (`POST /mcp`).

**Rationale**: Keeps `trading_mcp` decoupled from `price_feed_mcp` internals. Same JSON-RPC transport as all MCP calls. Easy to mock in tests with `respx`.

**Response shape**: `get_prices_latest` returns `{"ETH": "2000.00", ...}`. PriceClient returns `dict[str, Decimal]`.

### D3 — Alembic migrations via asyncio.to_thread

**Decision**: Wrap the synchronous Alembic `command.upgrade(cfg, "head")` call in `asyncio.to_thread()` inside the lifespan.

**Rationale**: Alembic's Python API is synchronous. The lifespan is async. `asyncio.to_thread` offloads the blocking work without blocking the event loop. The existing `env.py` already handles the async engine for actual migration execution.

**Alternative considered**: Call `alembic upgrade head` as a subprocess. Rejected — subprocess is fragile (PATH, cwd sensitivity); Python API is cleaner.

### D4 — LiveEngine as a configured skeleton

**Decision**: Implement `LiveEngine` with the correct structure (EIP-712 domain, struct types, `build_transaction`, `send_raw_transaction`, 60s confirmation poll) but guard execution behind a check: if `config.chain.risk_router_address` is empty/placeholder, raise `RuntimeError("Risk Router address not configured")`.

**Rationale**: The engine structure can be coded now from the ERC standards. The contract-specific ABI and address slots in config. This lets the live path be enabled on Day 1 by adding the address, with no code changes.

### D5 — ERC-8004 hooks as fire-and-forget tasks

**Decision**: After `engine.execute_swap()` returns `status="executed"`, call:
- `asyncio.create_task(registry.submit_validation(position_id))` on LONG
- `asyncio.create_task(registry.submit_reputation(position_id))` on FLAT (close)

Identity registration fires once in lifespan before serving requests.

**Rationale**: ERC-8004 hooks are already designed as non-blocking fire-and-forget (Tasks 10). The position_id is obtained from the DB after the trade write — paper engine returns the tx_hash; we fetch the position_id by asset+status query right after.

**Position ID lookup**: After a successful LONG, query `Position WHERE asset=x AND status='open' ORDER BY id DESC LIMIT 1`. After a successful FLAT, query `Position WHERE asset=x AND status='closed' ORDER BY closed_at DESC LIMIT 1`.

### D6 — get_portfolio / get_positions price fetching

**Decision**: `TradingService.get_portfolio(assets)` and `get_positions(assets)` accept the list of all tracked assets (from the MCP tool call), call `PriceClient.get_prices_latest(assets)`, then pass the dict to `PortfolioService`.

**Rationale**: `PortfolioService.get_portfolio(current_prices)` already takes `dict[str, Decimal]`. The service layer bridges the MCP tool (which receives the asset list) and the portfolio read.

### D7 — Test strategy

**Decision**: Integration test in `tests/trading_mcp/test_server.py` uses:
- SQLite in-memory for DB (same as existing engine/portfolio tests)
- `respx` to mock `price_feed_mcp` HTTP calls (same pattern as price_feed tests)
- FastMCP's `Client` (or direct tool call via service) to test end-to-end

**Rationale**: No real Postgres or chain needed. Tests remain fast and deterministic.

## Risks / Trade-offs

- **EIP-712 struct unknown**: The Risk Router's exact domain separator and struct fields are TBD. The live engine uses a reasonable standard structure (same as EIP-712 spec + TradeIntent fields). May need adjustment on Day 1. → Mitigation: `NotImplementedError` guard + clear TODO comment with struct placeholder.

- **Price fetch latency on every execute_swap**: The server fetches price from `price_feed_mcp` on every trade call. In paper mode this adds one HTTP round-trip. → Acceptable for hackathon scale (at most a few trades/hour).

- **Position ID lookup race**: After writing a position, we query by `asset + status` to get the ID for ERC-8004. There's a theoretical race if two LONGs on the same asset happen simultaneously. → Mitigated: `try_open_position` already uses an asyncio.Lock; only one LONG per asset can succeed at a time.

## Migration Plan

No data migration required — schema already created by Task 7. Alembic runs idempotently on startup (already at head → no-op).

Deployment: `python -m trading_mcp.server` or `uvicorn trading_mcp.server:mcp.http --port 8005`.

## Open Questions

- Risk Router ABI and address: provided on hackathon Day 1. Add to `config.yaml` and remove the guard in `LiveEngine`.
- EIP-712 domain name/version for TradeIntent struct: assume `"TradingAgent"` / `"1"` until confirmed.
