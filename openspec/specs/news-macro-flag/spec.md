# news-macro-flag Specification

## Purpose
Provides a global macro event flag derived from the same flat headline feed used for sentiment. The LLM assesses market-wide macro conditions without per-asset grouping.

## Requirements

### Requirement: Global macro event flag from LLM analysis
The system SHALL return a single global macro event flag: `{"triggered": bool, "reason": str|null}`. The flag SHALL be `true` when headlines indicate a market-wide macro event: Fed/central bank policy change, ETF approval or rejection, major exchange collapse or hack (>$100M), or government regulatory action targeting crypto broadly. The flag is intentionally global — it is not per-asset.

#### Scenario: Macro flag triggered on relevant headline
- **WHEN** `get_macro_flag()` is called and headlines contain an ETF approval or Fed rate decision
- **THEN** the response is `{"triggered": true, "reason": "<20-word explanation>"}`

#### Scenario: Macro flag not triggered on routine news
- **WHEN** `get_macro_flag()` is called and headlines contain only price movements and project updates
- **THEN** the response is `{"triggered": false, "reason": null}`

#### Scenario: Macro flag degrades gracefully on LLM failure
- **WHEN** the LLM call fails or returns unparseable JSON
- **THEN** the response is `{"triggered": false, "reason": null}` (no MCPError raised)

### Requirement: Macro flag uses flat headline feed as context
The system SHALL call `get_macro_flag()` with no arguments from the tool perspective. The server SHALL internally pass all `config.assets.tracked` assets to the service. The LLM SHALL receive the same flat headline list (capped at 50) used for sentiment scoring. No per-asset grouping is applied.

#### Scenario: Server passes tracked assets to service
- **WHEN** `get_macro_flag()` tool is called
- **THEN** the service receives the full tracked asset list from server config

#### Scenario: Macro flag assessed from flat feed
- **WHEN** `get_macro_flag()` triggers an LLM call
- **THEN** the LLM receives a flat list of all cached headlines (up to 50), not grouped by asset

### Requirement: Macro flag shares LLM call with sentiment via combined cache key
The macro flag result SHALL be cached at `news:analysis:macro` with TTL 300 seconds. The LLM call that produces sentiment scores also produces the macro flag in the same response. If `news:analysis:macro` is already cached, the LLM SHALL NOT be called solely for the macro flag.

#### Scenario: Macro flag cache hit skips LLM call
- **WHEN** `get_macro_flag()` is called and `news:analysis:macro` exists in Redis
- **THEN** the LLM is not called

#### Scenario: Macro flag written after LLM call
- **WHEN** the LLM is called (triggered by a sentiment cache miss) and returns a macro result
- **THEN** the macro result is stored at `news:analysis:macro` with TTL 300
