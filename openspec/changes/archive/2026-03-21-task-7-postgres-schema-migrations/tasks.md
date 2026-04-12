## 1. Dependencies

- [x] 1.1 Add `sqlalchemy[asyncio]>=2.0`, `asyncpg>=0.29`, `alembic>=1.13` to runtime deps in `pyproject.toml`
- [x] 1.2 Add `aiosqlite>=0.20` to `[dependency-groups] dev` in `pyproject.toml`
- [x] 1.3 Run `uv sync --all-groups` to install new deps

## 2. ORM Entity Models

- [x] 2.1 Create `src/trading_mcp/__init__.py`, `src/trading_mcp/domain/__init__.py`, `src/trading_mcp/domain/entity/__init__.py`
- [x] 2.2 Create `src/trading_mcp/domain/entity/base.py` with `class Base(DeclarativeBase): pass`
- [x] 2.3 Create `src/trading_mcp/domain/entity/agent.py` — `Agent` model (5 columns per §13)
- [x] 2.4 Create `src/trading_mcp/domain/entity/position.py` — `Position` model (22 columns per §13, correct nullability)
- [x] 2.5 Create `src/trading_mcp/domain/entity/trade.py` — `Trade` model (9 columns, FK→positions.id)
- [x] 2.6 Create `src/trading_mcp/domain/entity/portfolio_snapshot.py` — `PortfolioSnapshot` model (7 columns)
- [x] 2.7 Export all models from `src/trading_mcp/domain/entity/__init__.py`

## 3. DB Infrastructure

- [x] 3.1 Create `src/trading_mcp/infra/__init__.py`
- [x] 3.2 Create `src/trading_mcp/infra/db.py` with `make_engine(url)` and `make_session_factory(engine)`

## 4. Alembic Setup

- [x] 4.1 Create `src/trading_mcp/infra/alembic.ini` pointing `script_location` to `migrations`
- [x] 4.2 Create `src/trading_mcp/infra/migrations/env.py` using async pattern (`asyncio.run` + `engine.sync_engine`)
- [x] 4.3 Create `src/trading_mcp/infra/migrations/script.py.mako` (standard Alembic template)
- [x] 4.4 Create `src/trading_mcp/infra/migrations/versions/0001_initial_schema.py` — creates agents, positions, trades, portfolio_snapshots in FK-safe order; includes upgrade() and downgrade()

## 5. Tests

- [x] 5.1 Create `tests/trading_mcp/__init__.py`
- [x] 5.2 Create `tests/trading_mcp/test_schema.py` with table-driven tests:
  - Schema creation on SQLite in-memory succeeds
  - Agent model has expected columns and types
  - Position model has correct nullable/non-null flags for all 22 columns
  - Trade FK constraint raises IntegrityError on invalid position_id
  - PortfolioSnapshot insert succeeds without FK dependency

## 6. Verify

- [x] 6.1 Run `make lint` — no ruff errors
- [x] 6.2 Run `make test` — all tests pass
