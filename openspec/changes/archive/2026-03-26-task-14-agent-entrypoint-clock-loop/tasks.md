## 1. MCP Client Infra

- [x] 1.1 Create `src/agent/infra/__init__.py`
- [x] 1.2 Move `TradingMCPClient` from `src/agent/mcp_client.py` to `src/agent/infra/trading_mcp.py`; add `execute_swap(intent)` and `health_check()` methods
- [x] 1.3 Create `src/agent/infra/price_feed_mcp.py` — `PriceFeedMCPClient` with `get_prices(assets)` and `health_check()`
- [x] 1.4 Create `src/agent/infra/fear_greed_mcp.py` — `FearGreedMCPClient` with `get_index()` and `health_check()`
- [x] 1.5 Create `src/agent/infra/onchain_mcp.py` — `OnchainMCPClient` with `get_all(assets)` and `health_check()`
- [x] 1.6 Create `src/agent/infra/news_mcp.py` — `NewsMCPClient` with `get_signals(assets)` and `health_check()`
- [x] 1.7 Delete `src/agent/mcp_client.py`; update import in `src/agent/trigger/cooldown.py`

## 2. Clock Trigger Loop

- [x] 2.1 Create `src/agent/trigger/clock.py` — `ClockLoop` class with `run()` coroutine (15-min interval, immediate first tick)
- [x] 2.2 Implement early `get_portfolio()` call + position cap check — skip cycle if at cap or on error
- [x] 2.3 Implement cooldown pre-filter: compute `allowed_assets` synchronously after cap check
- [x] 2.4 Implement `_gather_signals(allowed_assets)` — fetches portfolio + prices/onchain/news for `allowed_assets` in parallel; on any exception log error and raise to skip cycle
- [x] 2.5 Implement intent submission loop: sort FLAT first, call `execute_swap`, call `record_trade` on success

## 3. Agent Entrypoint

- [x] 3.1 Create `src/agent/main.py` — `async def main()`: config load, logger init, parallel health checks (exit 1 on failure), portfolio load + log
- [x] 3.2 Add `asyncio.gather(clock_loop.run(), _noop_loop(), _noop_loop(), _noop_loop())` in `main()`; add `if __name__ == "__main__": asyncio.run(main())`

## 4. Tests

- [x] 4.1 Create `tests/agent/infra/__init__.py`
- [x] 4.2 `tests/agent/infra/test_trading_mcp.py` — JSON-RPC payload shape for `get_portfolio` and `execute_swap`; HTTP error and JSON-RPC error both raise `MCPError`; `health_check` returns `False` on connection error without raising
- [x] 4.3 `tests/agent/infra/test_price_feed_mcp.py` — `get_prices` maps response to `dict[str, PriceFeedData]`; `MCPError` on failure
- [x] 4.4 `tests/agent/infra/test_fear_greed_mcp.py` — `get_index` maps to `FearGreedData`; `MCPError` on failure
- [x] 4.5 `tests/agent/infra/test_onchain_mcp.py` — `get_all` merges parallel calls into `dict[str, OnchainData]`; `MCPError` on failure
- [x] 4.6 `tests/agent/infra/test_news_mcp.py` — `get_signals` merges sentiment + macro_flag into `dict[str, NewsData]`; `MCPError` on failure
- [x] 4.7 `tests/agent/trigger/test_clock.py` — cooldown suppresses asset from bundle; MCP gather failure skips cycle; position cap skips cycle; FLAT submitted before LONG; `record_trade` called on success, not called on `execute_swap` failure
- [x] 4.8 Update `tests/agent/trigger/test_cooldown.py` import path for `TradingMCPClient`
