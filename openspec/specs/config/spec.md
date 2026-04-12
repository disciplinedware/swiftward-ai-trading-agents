## Purpose
Configuration loading, validation, and caching for the Python trading agent. All MCP servers and the agent process share a single validated config object loaded from `config/config.yaml`.
## Requirements
### Requirement: Load and validate config from YAML
The system SHALL load configuration from a YAML file and validate it against a Pydantic model. `get_config()` SHALL return a validated config object. On validation failure it SHALL raise `ConfigError` with a human-readable message describing which field failed.

#### Scenario: Valid config file loads successfully
- **WHEN** `config/config.yaml` exists and all required fields are present with correct types
- **THEN** `get_config()` returns a fully populated config object with no error

#### Scenario: Missing required field raises ConfigError
- **WHEN** `config/config.yaml` is missing a required field (e.g. `llm.model`)
- **THEN** `get_config()` raises `ConfigError` naming the missing field

#### Scenario: Wrong type raises ConfigError
- **WHEN** a field has the wrong type (e.g. `trading.cooldown_minutes` is a string instead of int)
- **THEN** `get_config()` raises `ConfigError` with a descriptive message

#### Scenario: Config file not found raises ConfigError
- **WHEN** the config file does not exist at the resolved path
- **THEN** `get_config()` raises `ConfigError` stating the path that was not found

### Requirement: Config is cached after first load
The system SHALL load and validate the YAML file only once per process. Subsequent calls to `get_config()` SHALL return the same cached instance without re-reading the file.

#### Scenario: Second call returns cached instance
- **WHEN** `get_config()` is called twice
- **THEN** both calls return the same object instance (identity check passes)

#### Scenario: Cache can be reset for tests
- **WHEN** `_reset_config()` is called and then `get_config()` is called again
- **THEN** the YAML file is re-read and re-validated

### Requirement: Cache config section
The config model SHALL include a `cache` top-level section with a `redis_url` field (string). This section is shared by all MCP servers that use Redis caching.

#### Scenario: Valid cache config loads
- **WHEN** `config.yaml` contains `cache:\n  redis_url: redis://localhost:6379/0`
- **THEN** `get_config().cache.redis_url` returns `"redis://localhost:6379/0"`

#### Scenario: Missing cache section raises ConfigError
- **WHEN** `config.yaml` does not contain a `cache` section
- **THEN** `get_config()` raises `ConfigError` naming the missing field

### Requirement: Config file path is configurable via environment variable
The system SHALL resolve the config file path from the `AGENT_CONFIG_PATH` environment variable if set, otherwise default to `config/config.yaml` relative to the working directory.

#### Scenario: Custom path via env var
- **WHEN** `AGENT_CONFIG_PATH=/tmp/test_config.yaml` is set and that file exists
- **THEN** `get_config()` loads from `/tmp/test_config.yaml`

#### Scenario: Default path used when env var absent
- **WHEN** `AGENT_CONFIG_PATH` is not set
- **THEN** `get_config()` loads from `config/config.yaml` relative to cwd

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

