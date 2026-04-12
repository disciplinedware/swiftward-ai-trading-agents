## 1. Infrastructure — Postgres + Redis

- [x] 1.1 Add `py_trading` user and database to `postgres/init-databases.sh`
- [x] 1.2 Add `redis` service (image `redis:7-alpine`) with healthcheck and `redis_data` volume to `compose.infra.yaml`
- [x] 1.3 Add `redis_data` to the `volumes:` block in `compose.infra.yaml`

## 2. Python Config

- [x] 2.1 Create `python/config/config.yaml` from `config.example.yaml` with Docker container URLs: MCP server URLs use `py-*-mcp` hostnames, `trading.database_url` uses `py_trading` DB at `postgres`, `cache.redis_url` uses `redis://redis:6379/0`, LLM URLs use `swiftward-server:8093`

## 3. trading_mcp — Remove Migration from Lifespan

- [x] 3.1 Remove the Alembic migration block from `python/src/trading_mcp/server.py` lifespan (lines that call `_run_migrations` and the `_run_migrations` function itself); keep all other lifespan logic intact

## 4. compose.python.yaml

- [x] 4.1 Create `compose.python.yaml` with `name: ai-trading-agents` and `include: [compose.infra.yaml]`
- [x] 4.2 Add `py-price-feed-mcp` service: build from `./python`, command `uv run python -m price_feed_mcp.server`, port `8001:8001`, config volume, depends on `redis` healthy, healthcheck on `/health`
- [x] 4.3 Add `py-news-mcp` service: port `8002:8002`, same pattern
- [x] 4.4 Add `py-onchain-data-mcp` service: port `8003:8003`, same pattern
- [x] 4.5 Add `py-fear-greed-mcp` service: port `8004:8004`, same pattern
- [x] 4.6 Add `py-trading-mcp` service: port `8005:8005`, command `sh -c "uv run alembic -c src/trading_mcp/infra/alembic.ini upgrade head && uv run python -m trading_mcp.server"`, depends on `postgres` healthy + `redis` healthy, healthcheck on `/health`
- [x] 4.7 Add `py-agent` service: no port, command `uv run python -m agent.main`, depends on all five `py-*-mcp` services healthy, `restart: unless-stopped`

## 5. Wire into Full Stack

- [x] 5.1 Add `compose.python.yaml` to the `include:` list in `compose.yaml`
- [x] 5.2 Add `demo-python` target to `Makefile`: `docker compose -f compose.python.yaml up --build -d`
