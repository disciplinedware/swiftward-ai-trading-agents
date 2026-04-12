## Context

`fear_greed_mcp` is the fourth MCP server in the data layer (Task 4 of 24). It serves a single signal — the Crypto Fear & Greed Index from Alternative.me — used as a 20%-weighted input to the brain's Market Filter health score.

The existing `price_feed_mcp` server is the reference pattern: `infra/` for external HTTP clients, `service/` for business logic, `server.py` for FastMCP wiring. `common.cache.RedisCache` is shared across all MCP servers that need caching.

The original spec called for in-memory caching; we use Redis instead for consistency with `price_feed_mcp` (same infra, same `config.cache.redis_url`).

## Goals / Non-Goals

**Goals:**
- Serve `get_index` and `get_historical` tools via FastMCP on port 8004
- Cache `get_index` in Redis, keyed by UTC date — one fetch per calendar day
- Gracefully degrade to stale cached data when Alternative.me is unavailable
- Match the `price_feed_mcp` structural pattern exactly

**Non-Goals:**
- Streaming or push-based index updates
- Multiple index sources or fallback providers
- Sentiment computation (that's `news_mcp`)

## Decisions

### Redis cache with UTC-date invalidation (Option A: timestamp comparison)

On every `get_index` call:
1. Read `fear_greed:current` from Redis
2. If hit, compare stored `date` field against today's UTC date (`datetime.now(UTC).date()`)
3. Same date → return cached data (cache hit)
4. Different date → fetch fresh, overwrite Redis entry (midnight invalidation)
5. If miss → fetch fresh, write to Redis with TTL=25h (safety net against Redis TTL drift)

**Why timestamp comparison over a scheduled invalidation task:**
- No background asyncio task to manage or test
- Pure function logic — easy to unit test with a frozen clock
- Correct on cold starts (e.g., server restarts the next day)

### Graceful degradation to stale data

If Alternative.me fetch fails and Redis has a stale entry (any TTL, any date):
- Return the stale cached value (LLM gets a signal rather than a hard error)
- Log a warning with the staleness age

If Redis is also empty:
- Raise `MCPError("Fear & Greed index unavailable")` — FastMCP converts this to a JSON-RPC error

**Why stale-over-error for read-only signals:**
Fear & Greed changes slowly; a day-old value is far better than crashing the brain pipeline. The LLM can still reason with it. Hard errors should be reserved for cases where the data can't be trusted at all.

### `get_historical` — always fetch fresh

Historical data is used primarily for backtesting, not the live brain loop. No caching needed: the call is infrequent, and caching variable-length historical windows adds complexity without benefit.

### LLM-friendly response format

`get_index` returns:
```json
{"value": 65, "classification": "Greed", "updated_at": "2026-03-19T00:00:00Z"}
```
- `value` as `int` (not string) — directly comparable in LLM reasoning
- `classification` is the human-readable label from Alternative.me
- `updated_at` in ISO8601 UTC — unambiguous for the LLM

`get_historical` returns a list of the same shape with `timestamp` instead of `updated_at`.

## Risks / Trade-offs

- **Alternative.me availability** → Mitigated by stale-data fallback; this API is free and has been stable historically.
- **Redis unavailability** → `RedisCache` already handles this gracefully (falls through to `None`); server will refetch on every call but won't crash.
- **UTC date boundary edge case** → If the server processes a request exactly at midnight, it may fetch twice in quick succession. This is harmless — the second write just overwrites the first.

## Open Questions

None — all decisions resolved in explore session.
