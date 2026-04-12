## Why

Tasks 8–11 (portfolio state, paper engine, ERC-8004, trading server) all need a Postgres schema and ORM layer to persist positions, trades, and portfolio snapshots. This task establishes that foundation: four tables, SQLAlchemy 2.x ORM models, and an Alembic migration so startup can auto-upgrade the DB.

## What Changes

- Add `sqlalchemy[asyncio]`, `asyncpg`, `alembic` to runtime deps; `aiosqlite` to dev deps in `pyproject.toml`
- Create `src/trading_mcp/domain/entity/` with four ORM models: `Agent`, `Position`, `Trade`, `PortfolioSnapshot`
- Create `src/trading_mcp/infra/db.py` — async engine factory + session factory
- Create Alembic config (`src/trading_mcp/infra/alembic.ini`) and migration (`versions/0001_initial_schema.py`)
- Create `tests/trading_mcp/test_schema.py` — schema creation, FK constraints, column types (SQLite in-memory)

## Capabilities

### New Capabilities
- `trading-db-schema`: Four-table Postgres schema for trading-mcp owned by `trading_mcp` module. Covers agents (ERC-8004 identity), positions (open/closed), trades (execution log), and portfolio_snapshots (point-in-time state). Includes async engine/session factory and Alembic migrations.

### Modified Capabilities

## Impact

- `pyproject.toml`: new runtime and dev dependencies
- New module `src/trading_mcp/` created from scratch
- No existing code modified; no breaking changes
- Tests use `aiosqlite` in-memory — no Postgres required in CI
