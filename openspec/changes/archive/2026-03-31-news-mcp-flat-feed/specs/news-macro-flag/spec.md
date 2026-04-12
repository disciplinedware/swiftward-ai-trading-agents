## MODIFIED Requirements

### Requirement: Macro flag uses flat headline feed as context
The system SHALL call `get_macro_flag()` with no arguments. The server SHALL internally pass all `config.assets.tracked` assets to the service. The LLM SHALL receive the same flat headline list (capped at 50) used for sentiment scoring. No per-asset grouping is applied.

#### Scenario: Server passes tracked assets to service
- **WHEN** `get_macro_flag()` tool is called
- **THEN** the service receives the full tracked asset list from server config

#### Scenario: Macro flag assessed from flat feed
- **WHEN** `get_macro_flag()` triggers an LLM call
- **THEN** the LLM receives a flat list of all cached headlines (up to 50), not grouped by asset
