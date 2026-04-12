## ADDED Requirements

### Requirement: Cache config section
The config model SHALL include a `cache` top-level section with a `redis_url` field (string). This section is shared by all MCP servers that use Redis caching.

#### Scenario: Valid cache config loads
- **WHEN** `config.yaml` contains `cache:\n  redis_url: redis://localhost:6379/0`
- **THEN** `get_config().cache.redis_url` returns `"redis://localhost:6379/0"`

#### Scenario: Missing cache section raises ConfigError
- **WHEN** `config.yaml` does not contain a `cache` section
- **THEN** `get_config()` raises `ConfigError` naming the missing field
