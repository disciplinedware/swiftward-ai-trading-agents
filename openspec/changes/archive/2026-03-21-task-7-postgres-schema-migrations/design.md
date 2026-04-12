## Context

`trading_mcp` is the only component that owns the Postgres database. Tasks 8–11 build portfolio state, paper execution, ERC-8004 hooks, and the FastAPI server on top of this layer. Nothing exists under `src/trading_mcp/` yet.

The project uses Python 3.13 + asyncio throughout. All MCP servers follow a `server → service → infra` layering (see CLAUDE.md). Financial values are `Decimal`, never `float`.

## Goals / Non-Goals

**Goals:**
- Four SQLAlchemy 2.x ORM models matching requirements §13 exactly
- Async engine + session factory in `infra/db.py`
- Alembic migration that creates all four tables in FK-safe order
- Tests that verify schema correctness without a running Postgres instance

**Non-Goals:**
- No repository/service layer (Task 8)
- No FastAPI server wiring (Task 11)
- No data seeding or fixture data

## Decisions

### SQLAlchemy 2.x `Mapped[T]` style
Use `Mapped[T]` annotations with `mapped_column()` throughout. This is the modern, fully typed SQLAlchemy 2.x API. Rejected legacy `Column()` style — it's deprecated and produces untyped models.

### `DeclarativeBase` in `base.py`
Single `Base` class imported by all entity files and Alembic `env.py`. Keeps metadata in one place. Rejected `registry()` approach — unnecessary complexity for four models.

### `Numeric(precision, scale)` for financial values
`Numeric(20, 8)` for prices and USD amounts; `Numeric(10, 8)` for percentages (size_pct, slippage_pct, pnl_pct, drawdown_pct). Maps to Postgres `NUMERIC` and SQLite `NUMERIC`. Never `Float` — floating-point arithmetic is unacceptable for financial data.

### `DateTime(timezone=True)` for all timestamps
Maps to Postgres `TIMESTAMPTZ`. SQLite stores as text (ISO8601 with offset). Consistent with the `timestamptz` requirement in §13.

### Alembic async pattern
`env.py` uses `asyncio.run()` + `engine.sync_engine` to run migrations synchronously inside an async engine context — the standard Alembic async pattern. `run_migrations_online()` creates a new `AsyncEngine`, extracts its `sync_engine`, and passes it to `context.configure()`.

### SQLite in-memory for tests
Tests use `aiosqlite:///:memory:` — zero infrastructure, runs in CI without Postgres. `PRAGMA foreign_keys = ON` enabled via SQLAlchemy `@event.listens_for(engine.sync_engine, "connect")`. This enforces FK constraints in SQLite, which otherwise ignores them.

### Migration file: single `0001_initial_schema.py`
One migration for the initial schema (all four tables). Tables created in dependency order: `agents` → `positions` → `trades` (FK→positions) → `portfolio_snapshots`. Alembic `down_revision = None`.

### `db.py` API
```python
def make_engine(url: str) -> AsyncEngine: ...
def make_session_factory(engine: AsyncEngine) -> async_sessionmaker[AsyncSession]: ...
```
No global state in `db.py` — callers (server lifespan, tests) create and own the engine. This makes tests trivial: pass an in-memory URL, get an engine, run tests, discard.

## Risks / Trade-offs

[SQLite type mapping] → `Numeric` in SQLite stores as text/real, not a true fixed-precision type. Tests validate Python-level types and constraints, not DB-native precision. Postgres is the production target; SQLite is test-only. This is acceptable.

[Alembic autogenerate skipped] → The initial migration is hand-written (not `alembic revision --autogenerate`). This avoids needing a live DB during development. Risk: schema drift if models change. Mitigation: subsequent tasks use autogenerate against a dev DB.

## Migration Plan

Server startup (Task 11) calls `alembic upgrade head` via `alembic.command.upgrade(cfg, "head")` in the FastMCP lifespan before `yield`. This is synchronous and blocks startup until migrations complete — acceptable for a single-instance service.

No rollback strategy needed for initial schema (no data exists yet).

## Open Questions

None — all decisions made.
