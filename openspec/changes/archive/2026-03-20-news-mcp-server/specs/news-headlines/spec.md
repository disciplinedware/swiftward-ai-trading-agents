## ADDED Requirements

### Requirement: Headlines fetched from CryptoPanic per asset
The system SHALL fetch recent news headlines from CryptoPanic for a requested list of assets using the authenticated API (`auth_token` from config). Posts SHALL be filtered to `kind=news`. Each post returned by CryptoPanic is tagged with one or more currency codes; a post tagged `[BTC, ETH]` SHALL appear in the headline list for both BTC and ETH.

#### Scenario: Headlines returned grouped by asset
- **WHEN** `get_headlines(["BTC", "ETH"])` is called
- **THEN** the response is a dict with keys `"BTC"` and `"ETH"`, each containing a list of headline objects with fields `title`, `url`, `published_at`, `source`

#### Scenario: Asset with no headlines returns empty list
- **WHEN** CryptoPanic returns no posts tagged for a requested asset
- **THEN** that asset's entry in the response is an empty list `[]`

#### Scenario: CryptoPanic API error raises MCPError
- **WHEN** CryptoPanic returns a non-200 HTTP status or the request times out
- **THEN** the server raises `MCPError` with a descriptive message

### Requirement: Headlines cached per asset in Redis for 5 minutes
The system SHALL cache headline results per asset at key `news:headlines:{ASSET}` with TTL 300 seconds. On cache hit the CryptoPanic API SHALL NOT be called.

#### Scenario: Cache hit skips API call
- **WHEN** `get_headlines(["BTC"])` is called and `news:headlines:BTC` exists in Redis
- **THEN** CryptoPanic is not called and the cached value is returned

#### Scenario: Cache miss fetches and stores
- **WHEN** `get_headlines(["BTC"])` is called and `news:headlines:BTC` is absent from Redis
- **THEN** CryptoPanic is called, the result is stored at `news:headlines:BTC` with TTL 300, and the result is returned

#### Scenario: Partial cache hit fetches only uncached assets
- **WHEN** `get_headlines(["BTC", "ETH"])` is called and `news:headlines:BTC` is cached but `news:headlines:ETH` is not
- **THEN** CryptoPanic is called only for ETH, BTC is served from cache
