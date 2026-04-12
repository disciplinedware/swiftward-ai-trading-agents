## MODIFIED Requirements

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

### Requirement: LLM prompt uses flat headline list capped at 50 total
The system SHALL pass a flat list of at most 50 headlines to the LLM prompt, ordered by recency. Headlines SHALL NOT be pre-grouped by asset. The prompt SHALL list the assets to score so the LLM can self-assign relevance.

#### Scenario: Headline list is capped at 50
- **WHEN** the cache contains more than 50 headlines
- **THEN** only the first 50 are included in the LLM prompt

#### Scenario: Prompt includes asset list
- **WHEN** the LLM prompt is built
- **THEN** the assets to score are listed at the top of the prompt
