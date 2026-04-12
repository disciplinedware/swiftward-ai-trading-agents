## 1. Package scaffold

- [x] 1.1 Create `src/agent/trigger/__init__.py` (empty, makes package importable)
- [x] 1.2 Create `tests/agent/trigger/__init__.py` (empty)

## 2. Trading MCP client (minimal)

- [x] 2.1 Create `src/agent/mcp_client.py` with a `TradingMCPClient` class
- [x] 2.2 Constructor accepts `base_url: str` and an optional `httpx.AsyncClient` (injected for testing)
- [x] 2.3 Implement `async get_portfolio() -> PortfolioSnapshot` — raw JSON-RPC `POST /mcp` with `method: "get_portfolio"`, deserializes response into `PortfolioSnapshot` (already defined in `common/models/`)
- [x] 2.4 Raise `MCPError` on non-200 response or JSON-RPC error field

## 3. CooldownGate implementation

- [x] 3.1 Create `src/agent/trigger/cooldown.py` with `CooldownGate` class
- [x] 3.2 Constructor accepts `config: TradingConfig` and `mcp_client: TradingMCPClient`
- [x] 3.3 Implement `record_trade(asset: str) -> None` — sync, records `datetime.now(timezone.utc)` under the lock
- [x] 3.4 Implement private `_is_cooldown_open(asset: str) -> bool` — sync dict lookup, no lock needed (called only from within `is_allowed` under lock)
- [x] 3.5 Implement private `async _global_positions_ok() -> bool` — calls `mcp_client.get_portfolio()`, compares `open_position_count` vs `max_concurrent_positions`; returns `False` and logs warning on any exception
- [x] 3.6 Implement `async is_allowed(asset: str) -> bool` — acquires lock, checks `_is_cooldown_open` first (returns `False` early without MCP call if in cooldown), then awaits `_global_positions_ok()`

## 4. Tests

- [x] 4.1 Create `tests/agent/trigger/test_cooldown.py`; mock `TradingMCPClient.get_portfolio` with `AsyncMock`
- [x] 4.2 Test: per-asset isolation — `record_trade("BTC")` suppresses BTC but not ETH
- [x] 4.3 Test: expiry after cooldown window — use `time_machine` to advance time past `cooldown_minutes`; verify `is_allowed` returns `True`
- [x] 4.4 Test: gate closes immediately after `record_trade` — `is_allowed` returns `False` right after recording
- [x] 4.5 Test: no prior trade history → `is_allowed` returns `True` (mock returns 0 positions)
- [x] 4.6 Test: at max positions → `is_allowed` returns `False` even with no cooldown
- [x] 4.7 Test: MCP error → `is_allowed` returns `False`, no exception raised
- [x] 4.8 Test: cooldown check short-circuits MCP call — verify `mcp_client.get_portfolio` not awaited when asset is in cooldown (`AsyncMock` with `assert_not_awaited`)
- [x] 4.9 Run `make test` and confirm all tests pass
- [x] 4.10 Run `make lint` and fix any ruff findings

## 5. Progress tracking

- [x] 5.1 Update `python/docs/progress.md` — set Task 13 to `done`
