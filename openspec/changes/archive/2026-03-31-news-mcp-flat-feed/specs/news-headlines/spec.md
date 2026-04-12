## MODIFIED Requirements

### Requirement: Headlines fetched from CryptoPanic for all tracked assets
The system SHALL fetch recent news headlines from CryptoPanic for all tracked assets using the authenticated API (`auth_token` from config). Posts SHALL be filtered to `kind=news`. Posts SHALL NOT be tagged with currency codes — no currency field is populated.

#### Scenario: Headlines returned as flat list
- **WHEN** `get_headlines()` is called
- **THEN** the response is a flat list of headline objects with fields `title`, `url`, `published_at`, `source`

#### Scenario: CryptoPanic API error raises MCPError
- **WHEN** CryptoPanic returns a non-200 HTTP status or the request times out
- **THEN** the server raises `MCPError` with a descriptive message

### Requirement: All headlines cached under a single Redis key for 5 minutes
The system SHALL cache all fetched headlines at key `news:headlines:all` with TTL 300 seconds. On cache hit the CryptoPanic API SHALL NOT be called.

#### Scenario: Cache hit skips API call
- **WHEN** `get_headlines()` is called and `news:headlines:all` exists in Redis
- **THEN** CryptoPanic is not called and the cached list is returned

#### Scenario: Cache miss fetches and stores
- **WHEN** `get_headlines()` is called and `news:headlines:all` is absent from Redis
- **THEN** CryptoPanic is called, the result is stored at `news:headlines:all` with TTL 300, and the result is returned

## REMOVED Requirements

### Requirement: Headlines cached per asset in Redis for 5 minutes
**Reason**: Replaced by single `news:headlines:all` cache key — per-asset caching required currency tagging which is being removed.
**Migration**: Use `news:headlines:all` key; no per-asset invalidation.

### Requirement: Headlines fetched from CryptoPanic per asset
**Reason**: Replaced by flat feed — currency grouping is delegated to the LLM.
**Migration**: `get_headlines()` no longer accepts an `assets` parameter and returns a flat list instead of a grouped dict.
