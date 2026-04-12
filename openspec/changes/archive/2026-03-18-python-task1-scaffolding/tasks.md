## 1. Dependencies and config example

- [x] 1.1 Add `pyyaml` to `pyproject.toml` dependencies and run `uv sync`
- [x] 1.2 Create `config/config.example.yaml` with all keys and placeholder values (full structure from requirements §5: chain, assets, llm, trading, brain, mcp_servers, erc8004, external_apis, logging)
- [x] 1.3 Add `config/config.yaml` to `python/.gitignore`

## 2. Exceptions

- [x] 2.1 Create `src/common/__init__.py` (empty)
- [x] 2.2 Create `src/common/exceptions.py` with `AgentError`, `ConfigError`, `MCPError` hierarchy

## 3. Config loader

- [x] 3.1 Create Pydantic model classes in `src/common/config.py` matching `config.example.yaml` structure (nested models: `ChainConfig`, `LLMConfig`, `TradingConfig`, `BrainConfig`, `MCPServersConfig`, `ERC8004Config`, `ExternalAPIsConfig`, `LoggingConfig`, `AgentConfig`)
- [x] 3.2 Implement `get_config() -> AgentConfig` — resolves path from `AGENT_CONFIG_PATH` env var or default `config/config.yaml`, loads YAML, validates with Pydantic, caches result, raises `ConfigError` on any failure
- [x] 3.3 Implement `_reset_config()` — clears the module-level cache (for tests only)

## 4. Logger

- [x] 4.1 Create `src/common/log.py` with `setup_logging(config: AgentConfig)` — configures structlog globally with `ConsoleRenderer` (console) or `JSONRenderer` (json) based on `config.logging.format`; raises `ConfigError` for unknown format
- [x] 4.2 Implement `get_logger(name: str)` — returns a structlog logger bound with `logger=name`

## 5. Tests

- [x] 5.1 Create `tests/__init__.py` and `tests/common/__init__.py`
- [x] 5.2 Create `tests/common/test_config.py` — table-driven tests covering: valid config loads, missing required field raises ConfigError, wrong type raises ConfigError, file not found raises ConfigError, second call returns cached instance, `_reset_config()` forces reload, `AGENT_CONFIG_PATH` env var respected
- [x] 5.3 Create `tests/fixtures/config_valid.yaml` — minimal valid config used by tests (all required fields, no secrets)
- [x] 5.4 Create `tests/common/test_log.py` — tests covering: console format produces non-JSON output, json format produces parseable JSON per line, invalid format raises ConfigError
- [x] 5.5 Run `make test` — all tests pass
- [x] 5.6 Run `make lint` — no ruff errors
