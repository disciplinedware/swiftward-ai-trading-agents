## Context

Task 1 (scaffolding) and Task 2 (shared models) are complete. The project uses FastMCP (`mcp>=1.9.0` already installed), httpx for HTTP, Pydantic for config, and structlog for logging. `pandas` and `pandas-ta` are not yet installed. Redis is chosen for caching per the explore session (shared across all MCP servers that need TTL caches).

## Goals / Non-Goals

**Goals:**
- Expose `POST /mcp` via FastMCP (`stateless_http=True`) + `GET /health`
- Three tools: `get_prices_latest`, `get_prices_change`, `get_indicators`
- Fetch Binance public klines (no auth); map `BTC` → `BTCUSDT` using config stablecoin
- Cache raw klines per `(symbol, interval)` in Redis with 30s TTL
- Use `pandas-ta` for all indicator math (RSI14, EMA20/50/200, ATR14, BB20, volume ratio)
- Skip tests for indicator math correctness (library responsibility); test parsing + caching + wiring

**Non-Goals:**
- WebSocket streaming (polling-based only)
- Auth or rate-limit handling beyond what Binance's public tier allows
- Persisting price history (Redis is ephemeral cache only)

## Decisions

### D1: FastMCP with stateless_http=True

FastMCP provides `POST /mcp` JSON-RPC for free. `stateless_http=True` means no session state — each request is independent, which is correct for a polling-based server. Tools are defined with `@mcp.tool()` decorator. Health route added via `@mcp.custom_route("/health", methods=["GET"])`.

**Alternative**: Raw FastAPI + manual JSON-RPC parsing — rejected, more code, same result.

### D2: pandas-ta for indicators

`pandas-ta` integrates directly with pandas DataFrames and supports all required indicators in one call. No manual math needed. Tests verify the Binance → DataFrame → tool response pipeline, not the indicator formulas themselves.

**Alternative**: Manual numpy implementation — rejected, ~100 lines vs 5, and the plan's "test against known values" requirement was relaxed in the explore session.

### D3: Redis for caching

Cache key: `price_feed:{symbol}:{interval}` (e.g., `price_feed:BTCUSDT:15m`). Value: JSON-serialized kline list. TTL: 30 seconds. On cache miss, fetch from Binance and store. On Redis unavailability, fall through to Binance directly (no hard dependency).

**Alternative**: In-memory asyncio dict with TTL — rejected by user preference (Redis shared across processes, survives restarts).

### D4: Kline fetch strategy

| interval | limit | purpose |
|---|---|---|
| `1m` | 2 | 1m price change |
| `5m` | 2 | 5m price change |
| `15m` | 100 | RSI14, EMA20/50, ATR14, BB20, vol ratio |
| `1h` | 250 | EMA200 + derives 1h/4h/24h change from last 1/4/24 candles |

4h change = `(close[-1] - close[-5]) / close[-5]` on 1h klines. 24h change = `(close[-1] - close[-25]) / close[-25]`. This eliminates separate 4h/daily kline fetches.

### D5: Symbol mapping

Config uses short form (`BTC`). Binance requires `BTCUSDT`. Server appends config `assets.stablecoin` (default `USDC` but Binance uses `USDT` pairs). Note: Binance uses `USDT` not `USDC` for most pairs — stablecoin mapping: `USDC → USDT` for Binance symbol construction.

**Decision**: hardcode `USDT` suffix for Binance symbol construction regardless of config stablecoin. The config stablecoin controls the agent's portfolio currency, not Binance pair naming.

### D6: Tool response types

All prices and indicator values returned as `str` (Decimal-serialized). This is consistent with the shared models convention. Percentage changes as `str`. Booleans (e.g., BB squeeze detection) as `bool`.

## Risks / Trade-offs

- **Redis dependency**: If Redis is down, cache falls through to Binance. Under heavy polling (stop-loss loop every 2min × multiple assets) this is fine within Binance rate limits.
- **pandas-ta accuracy**: We trust the library. If a future indicator test fails due to library bugs, we'll add a known-value test then.
- **1m/5m windows via separate klines**: Adds 2 extra Binance calls per asset vs deriving from 15m. Worth the accuracy — 1m change from a 15m candle is meaningless.
- **EMA200 warm-up**: `pandas-ta` EMA200 needs ≥200 candles; we fetch 250 to be safe. First ~200 values are NaN — always use the last value.

## Open Questions

- None blocking implementation.
