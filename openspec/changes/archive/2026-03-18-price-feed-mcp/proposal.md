## Why

The agent brain and all trigger loops need live price data and technical indicators to make trading decisions. This MCP server is the most-used component — every other task from brain stages to stop-loss loop depends on it. It must exist before any data-consuming component can be built (Tasks 12–20).

## What Changes

- Add `src/price_feed_mcp/` FastAPI/FastMCP app on port 8001
- Add `pandas`, `pandas-ta`, `redis[asyncio]` to `pyproject.toml`
- Add `cache.redis_url` to `config/config.example.yaml` and Pydantic config model
- Tests: Binance response parsing, Redis cache hit/miss, symbol conversion, unknown asset error

## Capabilities

### New Capabilities

- `price-feed-mcp`: MCP server exposing three tools — `get_prices_latest`, `get_prices_change`, `get_indicators` — backed by Binance public REST API with Redis caching (30s TTL). Tools accept asset lists in config format (`["BTC", "ETH"]`), return structured dicts with `Decimal` values serialized as `str`.

### Modified Capabilities

- `config`: Add `cache.redis_url` field to the config schema (new top-level section shared by all MCP servers that need caching).

## Impact

- `src/price_feed_mcp/` — 5 new files
- `pyproject.toml` — 3 new dependencies (`pandas`, `pandas-ta`, `redis[asyncio]`)
- `config/config.example.yaml` — new `cache:` section
- `src/common/config.py` — new `CacheConfig` and `cache` field on `AgentConfig`
- `tests/price_feed_mcp/` — new test directory
