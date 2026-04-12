## Why

The Python trading agent has no runnable foundation — no config loading, no logging, no shared exceptions. Every subsequent component (MCP servers, agent brain, backtesting) imports from `src/common/`, so this must exist first.

## What Changes

- Add `config/config.example.yaml` — full config structure with all keys and placeholder values, committed to the repo
- Add `config/config.yaml` to `.gitignore`
- Add `src/common/config.py` — loads `config/config.yaml` via pyyaml, validates with a Pydantic model, exposes `get_config()`
- Add `src/common/log.py` — structlog-based logger, format toggled by `config.logging.format` (`console` | `json`)
- Add `src/common/exceptions.py` — base exception hierarchy (`AgentError`, `MCPError`, `ConfigError`)
- Update `pyproject.toml` — add `pyyaml` dependency

## Capabilities

### New Capabilities

- `config`: Load and validate agent configuration from a YAML file using a Pydantic model
- `logging`: Structured logging via structlog with console/JSON format toggle
- `exceptions`: Shared exception hierarchy for the agent and MCP servers

### Modified Capabilities

(none)

## Impact

- `pyproject.toml` — one new dependency (`pyyaml`)
- `config/` folder created at `python/` root
- `src/common/` module created — imported by all future components
- No breaking changes (nothing else exists yet)
