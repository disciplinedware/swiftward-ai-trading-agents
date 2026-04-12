## Why

The agent brain's Market Filter (Stage 1) needs a Fear & Greed index signal to compute its health score. `fear_greed_mcp` is the dedicated MCP server for this signal — the simplest of the five MCP servers, but a required dependency before the brain can run.

## What Changes

- New FastAPI/FastMCP service on port 8004
- Two MCP tools: `get_index` (daily-cached) and `get_historical` (always fresh)
- Alternative.me API client (no auth required)
- Redis cache with UTC-date-based invalidation (same infra as `price_feed_mcp`)

## Capabilities

### New Capabilities

- `fear-greed-index`: Fetch, cache, and serve the Crypto Fear & Greed Index from Alternative.me. Daily cache keyed by UTC date (invalidated at midnight), with graceful degradation to stale data when upstream is unavailable.

### Modified Capabilities

<!-- none -->

## Impact

- New package `src/fear_greed_mcp/` added to Python source tree
- No changes to existing modules
- Shares `common.cache.RedisCache` and `common.config.get_config()` with `price_feed_mcp`
- Requires `cache.redis_url` in `config.yaml` (already present)
- `docs/plan.md` Task 4 note updated: cache is Redis-backed (not in-memory as originally specced)
