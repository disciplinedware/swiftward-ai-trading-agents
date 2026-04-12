## ADDED Requirements

### Requirement: news_llm config section for lighter LLM model
The system SHALL support a `news_llm` top-level config section with fields `base_url`, `model`, `api_key`, and `max_tokens`. This config SHALL be used exclusively by the news MCP's LLM scorer and SHALL be independent of the brain's `llm` config to allow a different (cheaper/faster) model.

#### Scenario: news_llm config validated on startup
- **WHEN** the news MCP server starts
- **THEN** `get_config().news_llm` is available with all required fields populated

### Requirement: CryptoPanic API key in external_apis config
The config model `ExternalAPIsConfig` SHALL use field name `cryptopanic_api_key` (renamed from `news_api_key`) to explicitly name the chosen provider. The `config.example.yaml` SHALL include this field with a comment indicating it is required for the news MCP.

#### Scenario: cryptopanic_api_key present in config
- **WHEN** `config.yaml` contains `external_apis.cryptopanic_api_key: "abc123"`
- **THEN** `get_config().external_apis.cryptopanic_api_key` returns `"abc123"`

#### Scenario: Old news_api_key field rejected
- **WHEN** `config.yaml` contains `external_apis.news_api_key` instead of `cryptopanic_api_key`
- **THEN** config validation raises `ConfigError`
