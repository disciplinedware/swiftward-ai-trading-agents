## ADDED Requirements

### Requirement: ORM models match schema
The system SHALL provide SQLAlchemy 2.x ORM models for `Agent`, `Position`, `Trade`, and `PortfolioSnapshot` that exactly match the column definitions in requirements §13. All financial values SHALL use `Numeric` (never `Float`). All timestamps SHALL use `DateTime(timezone=True)`.

#### Scenario: Agent model columns
- **WHEN** the `Agent` ORM model is inspected
- **THEN** it has columns: id (Integer PK), agent_id (Integer), wallet_address (Text), registration_uri (Text), registered_at (DateTime tz)

#### Scenario: Position model columns
- **WHEN** the `Position` ORM model is inspected
- **THEN** it has all 22 columns from §13 with correct nullability: closed_at, exit_reason, exit_price, realized_pnl_usd, realized_pnl_pct, tx_hash_close, validation_uri are nullable; all others are NOT NULL

#### Scenario: Trade FK constraint
- **WHEN** a Trade row is inserted with a non-existent position_id
- **THEN** the database raises an IntegrityError

#### Scenario: Portfolio snapshot has no FK
- **WHEN** a PortfolioSnapshot row is inserted
- **THEN** it succeeds without referencing any other table

### Requirement: Async engine and session factory
The system SHALL expose `make_engine(url: str) -> AsyncEngine` and `make_session_factory(engine) -> async_sessionmaker` in `trading_mcp.infra.db`. These SHALL create no global state.

#### Scenario: Engine creation from URL
- **WHEN** `make_engine("sqlite+aiosqlite:///:memory:")` is called
- **THEN** it returns a usable `AsyncEngine`

#### Scenario: Session factory creates sessions
- **WHEN** `make_session_factory(engine)` is called and a session is opened
- **THEN** the session can execute queries against the schema

### Requirement: Alembic migration creates schema
The system SHALL include an Alembic migration `0001_initial_schema.py` that creates all four tables in FK-safe order. Running `alembic upgrade head` on a fresh database SHALL result in a schema identical to `Base.metadata.create_all()`.

#### Scenario: Upgrade from empty database
- **WHEN** `alembic upgrade head` is run against an empty database
- **THEN** all four tables exist with correct columns and constraints

#### Scenario: Downgrade removes all tables
- **WHEN** `alembic downgrade base` is run
- **THEN** all four tables are dropped

### Requirement: Tests use SQLite in-memory
The system SHALL test schema creation, FK constraint enforcement, and column types using `aiosqlite:///:memory:` with `PRAGMA foreign_keys = ON`. No live Postgres instance SHALL be required to run tests.

#### Scenario: Schema creation succeeds
- **WHEN** `Base.metadata.create_all()` is called on an in-memory SQLite engine
- **THEN** all four tables are created without error

#### Scenario: FK violation raises IntegrityError
- **WHEN** a Trade is inserted with a position_id that references no existing Position
- **THEN** an `IntegrityError` is raised
