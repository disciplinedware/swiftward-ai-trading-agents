### Requirement: Python MCP services run in Docker
All five Python MCP servers (price-feed, news, onchain-data, fear-greed, trading) SHALL be runnable as Docker containers via `compose.python.yaml`. Each service SHALL expose its port on the host, mount `./python/config` read-only, and declare a health check on its `/health` endpoint.

#### Scenario: MCP service starts healthy
- **WHEN** `docker compose -f compose.python.yaml up py-price-feed-mcp` is run
- **THEN** the service starts, passes its health check, and responds to `GET /health` with `{"status": "ok"}`

#### Scenario: Config is injected via volume mount
- **WHEN** any Python MCP service starts
- **THEN** it reads `config.yaml` from the mounted `/app/config` directory

### Requirement: Python agent runs in Docker
The Python trading agent (`py-agent`) SHALL run as a Docker container that starts only after all five MCP services are healthy.

#### Scenario: Agent waits for MCP services
- **WHEN** `docker compose -f compose.python.yaml up` is run
- **THEN** `py-agent` does not start until all five `py-*-mcp` services report healthy

### Requirement: Python services use dedicated database
The Python `trading_mcp` SHALL use the `py_trading` PostgreSQL database, separate from the Go `trading` database.

#### Scenario: Database created on first start
- **WHEN** the postgres container starts for the first time (fresh volume)
- **THEN** a `py_trading` database and `py_trading` user exist in PostgreSQL

#### Scenario: Alembic migrations run before server start
- **WHEN** `py-trading-mcp` container starts
- **THEN** `alembic upgrade head` runs to completion before the FastMCP server begins accepting connections

### Requirement: Redis available in shared infra
A Redis service SHALL be available in `compose.infra.yaml` for all Python MCP services that require caching.

#### Scenario: Redis reachable by Python services
- **WHEN** any Python MCP service configured with `cache.redis_url: redis://redis:6379/0` starts
- **THEN** it can connect to Redis using the `redis` hostname

### Requirement: Python stack runnable independently
The Python stack SHALL be runnable without starting Go or Rust agent services.

#### Scenario: Python-only demo
- **WHEN** `docker compose -f compose.python.yaml up` is run (without `compose.yaml`)
- **THEN** only infra + Python services start (no Go or Rust agents)
