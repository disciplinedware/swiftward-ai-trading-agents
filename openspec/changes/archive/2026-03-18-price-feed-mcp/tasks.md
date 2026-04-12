## 1. Dependencies & Config

- [x] 1.1 Add `pandas`, `pandas-ta`, `redis[asyncio]` to `pyproject.toml` dependencies; run `uv sync`
- [x] 1.2 Add `cache:` section to `config/config.example.yaml` with `redis_url: redis://localhost:6379/0`
- [x] 1.3 Add `CacheConfig(BaseModel)` with `redis_url: str` to `src/common/config.py`; add `cache: CacheConfig` field to `AgentConfig`
- [x] 1.4 Update existing config tests to include `cache:` section in test YAML fixtures

## 2. Package Skeleton

- [x] 2.1 Create `src/price_feed_mcp/__init__.py`
- [x] 2.2 Create `src/price_feed_mcp/cache.py` — async Redis wrapper: `get(key)`, `set(key, value, ttl)`, `close()`; falls through (returns None) if Redis unavailable

## 3. Binance Client

- [x] 3.1 Create `src/price_feed_mcp/binance.py` — async httpx client with methods:
  - `get_ticker_price(symbol: str) -> Decimal`
  - `get_klines(symbol: str, interval: str, limit: int) -> list[dict]`
- [x] 3.2 Symbol helper `asset_to_symbol(asset: str) -> str` — appends `"USDT"` (e.g. `"BTC"` → `"BTCUSDT"`)
- [x] 3.3 Raise `MCPError` on Binance HTTP errors or unexpected response shapes

## 4. Indicators

- [x] 4.1 Create `src/price_feed_mcp/indicators.py` — `compute_indicators(klines_15m, klines_1h) -> dict` using `pandas-ta`:
  - RSI 14, EMA 20/50 from 15m klines
  - EMA 200 from 1h klines
  - ATR 14, BB 20, volume ratio from 15m klines
  - All values returned as `str`-encoded Decimal (use last non-NaN value)

## 5. MCP Server

- [x] 5.1 Create `src/price_feed_mcp/server.py` — `FastMCP("price_feed", stateless_http=True)` instance with:
  - Lifespan context: initialize Redis cache and Binance client on startup, close on shutdown
  - `GET /health` via `@mcp.custom_route`
- [x] 5.2 Implement `get_prices_latest(assets: list[str]) -> dict[str, str]` tool:
  - Fetch Binance ticker price per asset (cache key: `price_feed:{symbol}:ticker`)
- [x] 5.3 Implement `get_prices_change(assets: list[str]) -> dict[str, dict[str, str]]` tool:
  - Fetch 1m klines (limit=2), 5m klines (limit=2), 1h klines (limit=250) per asset
  - Compute % change for each window; return `{"1m": "...", "5m": "...", "1h": "...", "4h": "...", "24h": "..."}`
- [x] 5.4 Implement `get_indicators(assets: list[str]) -> dict[str, dict]` tool:
  - Fetch 15m klines (limit=100) and 1h klines (limit=250) per asset (cached)
  - Call `compute_indicators()` and return result dict
- [x] 5.5 Add `if __name__ == "__main__":` entrypoint running `mcp.run(transport="streamable-http", host="0.0.0.0", port=8001)`

## 6. Tests

- [x] 6.1 Create `tests/price_feed_mcp/__init__.py`
- [x] 6.2 Create `tests/price_feed_mcp/test_binance.py` — test symbol mapping (`BTC → BTCUSDT`) and `MCPError` on bad Binance response (mock httpx)
- [x] 6.3 Create `tests/price_feed_mcp/test_cache.py` — test cache hit (Binance not called second time), cache miss after TTL, Redis-unavailable fallthrough (mock redis)
- [x] 6.4 Create `tests/price_feed_mcp/test_server.py` — test each tool returns correct response structure with mocked Binance + Redis; test `/health` endpoint
