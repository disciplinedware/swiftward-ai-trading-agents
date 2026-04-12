## 1. Package Skeleton

- [x] 1.1 Create `src/fear_greed_mcp/__init__.py`, `infra/__init__.py`, `service/__init__.py`
- [x] 1.2 Create `tests/fear_greed_mcp/__init__.py`

## 2. Infra Layer

- [x] 2.1 Implement `src/fear_greed_mcp/infra/alternative_me.py` — `AlternativeMeClient` with `connect()`, `close()`, `get_index()`, `get_historical(limit)` using httpx; raises `MCPError` on HTTP errors

## 3. Service Layer

- [x] 3.1 Implement `src/fear_greed_mcp/service/fear_greed.py` — `FearGreedService(client, cache)` with:
  - `get_index()` → Redis UTC-date cache logic + stale fallback
  - `get_historical(limit)` → always-fresh fetch, no caching
  - `_parse_entry()` pure helper: Alternative.me dict → `{value, classification, updated_at/timestamp}`

## 4. Server

- [x] 4.1 Implement `src/fear_greed_mcp/server.py` — FastMCP on port 8004, lifespan wires `AlternativeMeClient` + `RedisCache` → `FearGreedService`; tools `get_index` and `get_historical` are one-liners; `/health` route

## 5. Tests

- [x] 5.1 `tests/fear_greed_mcp/test_alternative_me.py` — mock HTTP: successful response parsing, HTTP error → MCPError, invalid JSON → MCPError
- [x] 5.2 `tests/fear_greed_mcp/test_service.py` — parametrized table covering: cache miss (fresh fetch + write), cache hit same UTC day (no fetch), stale cache different day (refetch + overwrite), upstream down + stale cache (return stale), upstream down + empty cache (MCPError); `get_historical` always fetches fresh

## 6. Docs

- [x] 6.1 Update `docs/plan.md` Task 4: change "cached in memory" to "cached in Redis (UTC-date key, TTL=25h)"
