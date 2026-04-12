## Context

The Python agent is a blank slate (`pyproject.toml` + `Dockerfile`, no `src/` code). Every component that will be built — MCP servers, agent brain, backtesting — imports from `src/common/`. This design establishes the three pillars of that module: config, logging, exceptions.

## Goals / Non-Goals

**Goals:**
- Load `config/config.yaml` at startup and validate its shape with Pydantic
- Provide a single `get_config()` call that all modules use — no repeated YAML loading
- Provide a `get_logger(name)` call that returns a structlog logger bound to the caller's name
- Provide a base exception hierarchy that MCP servers and agent code can raise and catch

**Non-Goals:**
- Environment variable substitution or override (secrets go directly in `config.yaml`)
- Hot-reload of config at runtime
- Logging to file or external sink (stdout only)
- Config migration or versioning

## Decisions

### Pydantic model for config validation (not dataclasses, not pydantic-settings)

**Decision**: Use a plain `pydantic.BaseModel` hierarchy to validate the parsed YAML dict.

**Rationale**: Pydantic gives free type coercion and clear error messages on bad YAML structure. `pydantic-settings` is designed for env-var loading — not needed here since all config comes from a single YAML file. Dataclasses give no validation.

**Alternative considered**: `pydantic-settings` with a custom YAML source. Adds complexity for no benefit — the env-var layer of pydantic-settings is unused.

### Singleton config via module-level cache

**Decision**: `get_config()` loads and validates once, caches the result in a module-level variable.

```
first call → read YAML → parse → validate (Pydantic) → cache → return
subsequent calls → return cached instance
```

**Rationale**: Config doesn't change at runtime. Repeated file reads add noise. Tests can call `_reset_config()` (internal) to clear cache between cases.

### Config file path from environment, defaulting to `config/config.yaml`

**Decision**: `get_config()` checks `AGENT_CONFIG_PATH` env var, falls back to `config/config.yaml` relative to the working directory.

**Rationale**: Makes it easy to point tests at a fixture YAML without touching the real config. Single env var, no magic.

### structlog with two processors: console and JSON

**Decision**: `log.py` configures structlog globally once. `get_logger(name)` returns a bound logger.

- `console`: `structlog.dev.ConsoleRenderer` — human-readable, colored output
- `json`: `structlog.processors.JSONRenderer` — one JSON object per line

Format is read from config at logger init time. Logger init must be called after `get_config()`.

## Risks / Trade-offs

- **Config loaded before logger is ready** → `config.py` uses `print()` only for a fatal load error, then raises `ConfigError`. Normal path has no output from config loading itself.
- **Module-level singleton is not thread-safe** → Not a concern: the agent is asyncio-based, single-threaded event loop. Config is always loaded at startup before any concurrent code runs.
- **Pydantic model must stay in sync with `config.example.yaml`** → The example YAML is the canonical reference. If the model and example diverge, tests will catch it (test loads the example YAML through the model).
