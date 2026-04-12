## Why

The Python MCP services and agent have no Docker Compose setup, so they can only be run locally. Adding `compose.python.yaml` brings Python into the full-stack demo alongside Go and Rust.

## What Changes

- Add `py_trading` PostgreSQL database to `postgres/init-databases.sh`
- Add shared `redis` service to `compose.infra.yaml`
- Create `python/config/config.yaml` with Docker-aware container URLs and placeholder secrets
- Create `compose.python.yaml` with 6 services: `py-price-feed-mcp`, `py-news-mcp`, `py-onchain-data-mcp`, `py-fear-greed-mcp`, `py-trading-mcp`, `py-agent`
- Remove Alembic migration from `trading_mcp` lifespan; run it via the service's `command` instead
- Add `compose.python.yaml` to `compose.yaml` includes
- Add `demo-python` target to `Makefile`

## Capabilities

### New Capabilities

- `python-compose`: Docker Compose configuration for all Python MCP services and the Python trading agent

### Modified Capabilities

- (none)

## Impact

- `postgres/init-databases.sh` — adds new user/database (affects all contributors on first `docker compose up` after reset)
- `compose.infra.yaml` — adds Redis (available to all compose stacks that include infra)
- `python/src/trading_mcp/server.py` — removes migration from lifespan (behavior change: migration runs before server start, not during)
- `python/config/config.yaml` — new gitignored file, not committed
