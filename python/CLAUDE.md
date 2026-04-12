# CLAUDE.md — Python Trading Agent

## Commands

```bash
make install   # uv sync --all-groups
make lint      # ruff check .
make test      # pytest -v
```

## Project Layout

```
python/
├── config/
│   ├── config.example.yaml   # committed — all keys with placeholders
│   └── config.yaml           # gitignored — actual secrets/values
├── src/
│   ├── common/
│   │   ├── config.py         # loads config.yaml, exposes get_config()
│   │   ├── log.py            # structlog setup
│   │   ├── exceptions.py     # AgentError, MCPError, ConfigError
│   │   └── models/           # TradeIntent, Position, SignalBundle
│   ├── agent/
│   │   ├── brain/            # Stage 1 market filter, Stage 2 rotation, Stage 3 decision
│   │   ├── loops/            # clock (15m), price_spike (1m), stop_loss (2m), tier2 (5m)
│   │   ├── trigger/          # CooldownGate
│   │   └── mcp_client.py     # JSON-RPC HTTP client for all MCP servers
│   ├── mcp_servers/
│   │   ├── price_feed/       # port 8001 — OHLCV, indicators (Binance)
│   │   ├── news/             # port 8002 — headlines, sentiment, macro flag
│   │   ├── onchain_data/     # port 8003 — funding, OI, liquidations, netflow
│   │   ├── fear_greed/       # port 8004 — Alternative.me index, daily cache
│   │   └── trading/          # port 8005 — execution, portfolio, ERC-8004 hooks
│   └── backtesting/          # runner, MCP stubs, metrics, data download
└── tests/
```

Python agent details and shipped features: see `docs/plans/completed/python-agent-go-trading-mcp.md`.

## Hard Architecture Rules

- **Agent never calls external APIs directly** — always via MCP servers
- **Agent never touches Postgres** — only MCPs owns the DB
- **All MCP calls use `POST /mcp` JSON-RPC** — polling loops and LLM tool calls are identical
  in transport; no special client needed, just plain HTTP POST with a JSON-RPC body
- **ERC-8004 hooks are internal to `trading-mcp`** — agent never calls registries directly
- **SwiftWard is invisible to the agent** — it's just a URL in config (`trading_url`).
  Agent handles `approved`, `modified`, `rejected` responses from the standard `/execute` route.

## MCP Server Structure

Each MCP server follows the same internal layout (see `price_feed_mcp` as the reference):

```
{name}_mcp/
├── infra/          # external clients only (HTTP APIs, DBs, third-party SDKs)
│   └── {client}.py
├── service/        # business logic: orchestration, caching, computation
│   ├── {name}.py   # XxxService class — injected with infra deps via constructor
│   └── {util}.py   # pure functions (indicators, formatters, etc.)
└── server.py       # FastMCP setup: lifespan wires deps, @mcp.tool() handlers are one-liners
```

Rules:
- **`server.py` is presentation only** — no logic, no I/O. Each tool handler calls one service method.
- **`service/` has no knowledge of FastMCP** — testable without starting the server.
- **`infra/` has no knowledge of service** — only talks to the external system.
- **Deps flow inward**: `server → service → infra`. Never the reverse.

## Python Conventions

**Toolchain**: `uv` for dependency management (not pip, not poetry). `ruff` for linting (not
flake8, not pylint). `pytest` with `asyncio_mode = auto`.

**Async**: everything is asyncio. No sync blocking calls in async context. Use `asyncio.Lock`
for shared mutable state (cooldown gate, caches).

**Models**: `src/common/models/` contains `TradeIntent`, `Position`, `SignalBundle` as Pydantic
`BaseModel` subclasses (not `@dataclass`). Import from `common.models`. `Decimal` fields
serialize to `str` in `model_dump()` and accept `str | Decimal | int | float` in
`model_validate()`. Sub-classes in `SignalBundle` (`PriceFeedData`, etc.) are stubs until MCP
tasks fill them in.

**Financial values**: always `decimal.Decimal` for prices, sizes, PnL, balances. Never `float`.
Convert at boundaries (config stays float, convert when assigning to model fields).

**Logging**: `structlog` only. No `print`, no `logging.getLogger`. Use `from common.log import get_logger; logger = get_logger(__name__)` everywhere — including inside `src/common/` itself. Format toggled by `config.logging.format`: `console` for local dev, `json` for VPS/prod.

**Config**: `src/common/config.py` loads `config/config.yaml` via pyyaml. No env var
substitution — all values including secrets go directly in `config.yaml`.

**Testing style**: `pytest.mark.parametrize` table-driven (main scenarios + key edge cases in
one test function). One test file per module. Async tests get `asyncio_mode = auto` for free.

## Development workflow

This subsystem uses the OpenSpec workflow (`openspec/` at the repo root). All commands run
from the repo root, never from `python/`. The canonical openspec directory is `openspec/` at
the repo root — never create `python/openspec/`.
