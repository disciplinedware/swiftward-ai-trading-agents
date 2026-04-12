## ADDED Requirements

### Requirement: get_index returns current Fear & Greed value
The server SHALL expose a `get_index` MCP tool that returns the current Crypto Fear & Greed Index value (0–100), its classification label, and the UTC timestamp of the data point.

#### Scenario: Fresh fetch when cache is empty
- **WHEN** `get_index` is called and Redis has no cached entry
- **THEN** the server fetches from Alternative.me, caches the result with TTL=25h, and returns `{value, classification, updated_at}`

#### Scenario: Cache hit on same UTC day
- **WHEN** `get_index` is called and Redis has a cached entry dated today (UTC)
- **THEN** the server returns the cached value without calling Alternative.me

#### Scenario: Cache invalidated at UTC midnight
- **WHEN** `get_index` is called and Redis has a cached entry dated before today (UTC)
- **THEN** the server fetches fresh data from Alternative.me, overwrites the cache entry, and returns the new value

#### Scenario: Graceful degradation with stale data
- **WHEN** `get_index` is called, Alternative.me is unreachable, and Redis has a stale entry (any date)
- **THEN** the server returns the stale cached value and logs a warning

#### Scenario: Hard failure when no data available
- **WHEN** `get_index` is called, Alternative.me is unreachable, and Redis has no entry
- **THEN** the server raises MCPError("Fear & Greed index unavailable")

### Requirement: get_historical returns last N daily values
The server SHALL expose a `get_historical` MCP tool that accepts a `limit: int` parameter and returns that many daily Fear & Greed values, always fetched fresh from Alternative.me.

#### Scenario: Returns requested number of entries
- **WHEN** `get_historical(limit=7)` is called
- **THEN** the server returns a list of 7 entries, each with `value`, `classification`, and `timestamp` (ISO8601 UTC)

#### Scenario: No caching for historical data
- **WHEN** `get_historical` is called twice in succession
- **THEN** the server calls Alternative.me both times (no cache read or write)

### Requirement: Server exposes health endpoint
The server SHALL expose `GET /health` returning `{"status": "ok"}` for liveness checks by the agent startup sequence.

#### Scenario: Health check returns 200
- **WHEN** `GET /health` is requested
- **THEN** the server returns HTTP 200 with `{"status": "ok"}`
