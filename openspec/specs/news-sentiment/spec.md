# news-sentiment Specification

## Purpose
Provides per-asset sentiment scores derived from a single LLM call over a flat headline feed. The LLM infers which headlines are relevant to each asset without pre-grouping.

## Requirements

### Requirement: Per-asset sentiment scores produced by batched LLM call
The system SHALL return a sentiment score in the range [-1.0, +1.0] for each requested asset. Scores SHALL be computed by a single LLM call that receives all tracked assets and all cached headlines in one flat prompt. The LLM SHALL infer which headlines are relevant to each asset without pre-grouping. The LLM SHALL be instructed to output strict JSON: `{"sentiment": {"BTC": 0.3, ...}, "macro": {"triggered": bool, "reason": str|null}}`. Assets with no relevant headlines SHALL receive a score of 0.0.

#### Scenario: Sentiment returned for all requested assets
- **WHEN** `get_sentiment(["BTC", "ETH", "SOL"])` is called
- **THEN** the response is `{"BTC": <float>, "ETH": <float>, "SOL": <float>}` with each value clamped to [-1.0, +1.0]

#### Scenario: LLM failure raises MCPError
- **WHEN** the LLM call raises an exception or returns unparseable JSON
- **THEN** `MCPError` is raised (no silent fallback to 0.0)

#### Scenario: LLM scores are clamped to valid range
- **WHEN** the LLM returns a score outside [-1.0, +1.0] for any asset
- **THEN** the score is clamped to the nearest bound before being returned

### Requirement: Sentiment cached per asset in Redis for 5 minutes
The system SHALL cache the sentiment score for each asset at key `news:analysis:{ASSET}` with TTL 300 seconds. The LLM SHALL NOT be called for assets that have a valid cached score. The single LLM call covers only the uncached assets; results are fanned out to individual per-asset cache keys on write.

#### Scenario: Cache hit skips LLM call
- **WHEN** `get_sentiment(["BTC"])` is called and `news:analysis:BTC` exists in Redis
- **THEN** the LLM is not called and the cached score is returned

#### Scenario: Partial cache hit calls LLM only for uncached assets
- **WHEN** `get_sentiment(["BTC", "ETH"])` is called, `news:analysis:BTC` is cached, and `news:analysis:ETH` is not
- **THEN** the LLM is called with only ETH in scope, ETH result is stored at `news:analysis:ETH`, and BTC is served from cache

#### Scenario: Results stored per asset after LLM call
- **WHEN** `get_sentiment(["BTC", "ETH"])` triggers an LLM call for both assets
- **THEN** `news:analysis:BTC` and `news:analysis:ETH` are each written to Redis with TTL 300

### Requirement: LLM prompt uses flat headline list capped at 50 total
The system SHALL pass a flat list of at most 50 headlines to the LLM prompt, ordered by recency. Headlines SHALL NOT be pre-grouped by asset. The prompt SHALL list the assets to score so the LLM can self-assign relevance.

#### Scenario: Headline list is capped at 50
- **WHEN** the cache contains more than 50 headlines
- **THEN** only the first 50 are included in the LLM prompt

#### Scenario: Prompt includes asset list
- **WHEN** the LLM prompt is built
- **THEN** the assets to score are listed at the top of the prompt
