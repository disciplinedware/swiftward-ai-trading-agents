# Network Topology

## Two networks

| Network | Type | Purpose |
|---------|------|---------|
| `default` | normal | Infrastructure, dashboards, internet access |
| `agent-isolated` | internal (no internet) | Agent containers - no outbound internet |

## How agent isolation works

Agents on `agent-isolated` can only reach services that are also on that network. They cannot reach the internet directly. Claude agents use `HTTP_PROXY`/`HTTPS_PROXY` to route outbound traffic through the Swiftward Internet Gateway (swiftward-server:8097), which applies policy rules.

## Service network membership

```
default network (internet access)
  signoz-clickhouse
  signoz-zookeeper
  signoz (UI :3301)
  redis
  classifiers
  agent-random
  agent-gamma-java (Java agent — currently on default network)

default + agent-isolated (bridge services)
  postgres
  swiftward-server (MCP GW :8095, LLM GW :8093, Inet GW :8097, Control UI :5174)
  trading-server (Dashboard :8091, Evidence API :8092)
  signoz-otel-collector (OTLP :4317/:4318)
  agent-ruby-solid (admin UI :7175 - on default for port mapping)

agent-isolated only (no internet)
  agent-alpha-claude (Claude)
  agent-gamma-claude (Claude)
  agent-ruby-solid-worker (GoodJob - does the actual trading)
```

## Data flow

```
Agent (isolated)
  -> swiftward-server:8095 (MCP Gateway, on both networks)
    -> trading-server:8091 (backend, on both networks)
      -> external APIs (binance, bybit - from default network)

Claude agents additionally:
  -> swiftward-server:8093 (LLM Gateway)
    -> OpenAI/Anthropic APIs
  -> swiftward-server:8097 (Internet Gateway, HTTP proxy)
    -> filtered internet access
```

## Port mapping and internal networks

Docker cannot bind host ports for containers exclusively on an internal network. Services that need host-accessible ports (dashboards, admin UIs) must also be on the `default` network. The `agent-ruby-solid` web service is on both networks for this reason - its admin UI is at :7175. The worker (which does actual trading) stays isolated.

## Postgres databases

Shared Postgres instance with per-service logical databases:

| Database | User | Service |
|----------|------|---------|
| `swiftward` | `swiftward` | swiftward-server |
| `trading` | `trading` | trading-server |
| `py_trading` | `py_trading` | Python agents |
| `solid_loop_trading_production` | `solid_loop_trading` | Ruby agent (main) |
| `solid_loop_trading_production_cache` | `solid_loop_trading` | Ruby agent (Solid Cache) |
| `solid_loop_trading_production_cable` | `solid_loop_trading` | Ruby agent (Solid Cable) |

Created by `postgres/init-databases.sh` on first Postgres init. For existing installations, create manually (see below).

## Adding databases to existing Postgres

If Postgres already has data (`data/postgres` exists), `init-databases.sh` won't re-run. Create manually:

```bash
docker compose exec postgres psql -U admin -d postgres -c "CREATE USER solid_loop_trading WITH PASSWORD 'solid_loop_trading';"
docker compose exec postgres psql -U admin -d postgres -c "CREATE DATABASE solid_loop_trading_production OWNER solid_loop_trading;"
docker compose exec postgres psql -U admin -d postgres -c "CREATE DATABASE solid_loop_trading_production_cache OWNER solid_loop_trading;"
docker compose exec postgres psql -U admin -d postgres -c "CREATE DATABASE solid_loop_trading_production_cable OWNER solid_loop_trading;"
```
