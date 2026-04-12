## Context

Go and Rust each have a `compose.{lang}.yaml` that includes `compose.infra.yaml`. Python has a `Dockerfile` and a full set of FastMCP services but no compose file. Redis is needed by all Python MCP servers for caching but is not in shared infra. The Python `trading_mcp` runs Alembic migrations inside its lifespan hook, which is non-standard — other migration patterns in this repo use separate one-shot services or startup commands.

## Goals / Non-Goals

**Goals:**
- Follow the existing `compose.{lang}.yaml` + `compose.infra.yaml` include pattern
- All 5 Python MCP services and the agent runnable via `docker compose -f compose.python.yaml up`
- Redis in shared infra (available to all stacks)
- Python `trading_mcp` uses a dedicated `py_trading` DB (separate from Go's `trading` DB — both have a `trades` table with incompatible schemas)
- Alembic runs before server start, not inside lifespan code

**Non-Goals:**
- Python agent registration in trading-server or swiftward (no auth/policy enforcement for now)
- Merging Python trading schema with Go trading schema
- Env-var-based config (Python config stays YAML-file-based)

## Decisions

**D1 — Separate `py_trading` database (not shared with Go's `trading`)**
Both Go's trading-server and Python's trading_mcp create a `trades` table with different schemas. Using the same DB would cause Alembic to error on existing tables or corrupt Go's data. Using `py_trading` keeps them fully isolated.
*Alternative considered: prefix tables (e.g., `py_trades`) — rejected, requires code changes in Python trading_mcp.*

**D2 — Redis in `compose.infra.yaml` (shared infra)**
Redis is not Go/Rust-specific infra — it's a caching layer that other language agents might use. Placing it in infra avoids duplicating it if a Ruby or Rust agent adds Redis caching later.
*Alternative: Redis in `compose.python.yaml` — simpler but creates ownership confusion if reused.*

**D3 — Alembic via `command`, not lifespan**
The `py-trading-mcp` service command becomes:
```
sh -c "uv run alembic -c src/trading_mcp/infra/alembic.ini upgrade head && uv run python -m trading_mcp.server"
```
The lifespan migration code is removed from `server.py`. This matches standard practice (migrations are an ops concern, not an application concern) and makes the migration step visible in compose logs.
*Alternative: separate one-shot migration service — more compose boilerplate for a single-server setup.*

**D4 — `py-{folder-name}` service naming**
Service names match Python module folder names with `py-` prefix and underscores→dashes: `price_feed_mcp` → `py-price-feed-mcp`. Consistent, discoverable, avoids collision with Go/Rust service names.

**D5 — Config file mounted as volume**
`./python/config:/app/config:ro` is mounted into every Python service. The `config.yaml` in that directory is gitignored (has secrets). A `config.yaml` with Docker container URLs and placeholder secrets is created alongside the existing `config.example.yaml`.
*Alternative: env var overrides — requires significant config.py changes; not worth it for a hackathon.*

## Risks / Trade-offs

- **config.yaml is not committed** — new contributor needs to create it manually. Mitigated by `config.example.yaml` with Docker URLs and clear comments.
- **Redis in shared infra adds a container for Go/Rust-only stacks** — minimal overhead, Redis is lightweight.
- **`sh -c "... && ..."` command is fragile if alembic fails** — if migration fails, server doesn't start and compose restarts the container. This is the desired behavior (fail fast).
- **`postgres_data` volume must be deleted to pick up new `py_trading` DB** — `init-databases.sh` runs only once on first volume creation. Existing deployments need `make postgres-reset` or manual `docker volume rm`.

## Migration Plan

1. Merge changes (infra files first: `init-databases.sh`, `compose.infra.yaml`)
2. For existing deployments: `docker compose down -v` or `make postgres-reset` to recreate postgres volume with `py_trading` DB
3. Copy `python/config/config.example.yaml` → `python/config/config.yaml`, fill in secrets
4. `docker compose -f compose.python.yaml up --build`

## Open Questions

- (none — all decisions made during explore phase)
