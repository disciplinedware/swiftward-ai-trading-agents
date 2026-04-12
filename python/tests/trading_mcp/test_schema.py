"""Schema tests for trading_mcp ORM models.

Uses SQLite in-memory with PRAGMA foreign_keys = ON.
No live Postgres required.
"""
from datetime import datetime, timezone
from decimal import Decimal

import pytest
import sqlalchemy as sa
from sqlalchemy import event
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncSession

from trading_mcp.domain.entity import Agent, Base, PortfolioSnapshot, Position, Trade
from trading_mcp.infra.db import make_engine, make_session_factory


@pytest.fixture
async def session():
    engine = make_engine("sqlite+aiosqlite:///:memory:")

    # Enable FK enforcement for SQLite
    @event.listens_for(engine.sync_engine, "connect")
    def set_sqlite_pragma(dbapi_conn, _):
        cursor = dbapi_conn.cursor()
        cursor.execute("PRAGMA foreign_keys=ON")
        cursor.close()

    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    factory = make_session_factory(engine)
    async with factory() as s:
        yield s

    await engine.dispose()


# ---------------------------------------------------------------------------
# Schema creation
# ---------------------------------------------------------------------------


async def test_schema_creates_all_tables(session: AsyncSession):
    result = await session.execute(sa.text("SELECT name FROM sqlite_master WHERE type='table'"))
    tables = {row[0] for row in result}
    assert {"agents", "positions", "trades", "portfolio_snapshots"}.issubset(tables)


# ---------------------------------------------------------------------------
# Column presence and nullability — table-driven
# ---------------------------------------------------------------------------

_POSITION_NULLABLE = {
    "validation_uri",
    "closed_at",
    "exit_reason",
    "exit_price",
    "realized_pnl_usd",
    "realized_pnl_pct",
    "tx_hash_close",
}

_POSITION_NOT_NULL = {
    "id",
    "asset",
    "status",
    "action",
    "entry_price",
    "size_usd",
    "size_pct",
    "stop_loss",
    "take_profit",
    "strategy",
    "trigger_reason",
    "reasoning",
    "opened_at",
    "tx_hash_open",
}


@pytest.mark.parametrize(
    "col_name,expected_nullable",
    [(c, True) for c in _POSITION_NULLABLE] + [(c, False) for c in _POSITION_NOT_NULL],
)
def test_position_column_nullability(col_name: str, expected_nullable: bool):
    col = Position.__table__.c[col_name]
    assert col.nullable == expected_nullable, (
        f"Column {col_name!r}: expected nullable={expected_nullable}, got {col.nullable}"
    )


@pytest.mark.parametrize(
    "col_name",
    ["entry_price", "size_usd", "size_pct", "stop_loss", "take_profit"],
)
def test_position_numeric_columns(col_name: str):
    col = Position.__table__.c[col_name]
    assert isinstance(col.type, sa.Numeric), f"{col_name} should be Numeric"


def test_agent_columns():
    cols = {c.name for c in Agent.__table__.c}
    assert cols == {"id", "agent_id", "wallet_address", "registration_uri", "registered_at"}


def test_trade_has_fk_to_positions():
    fks = list(Trade.__table__.foreign_keys)
    assert len(fks) == 1
    fk = fks[0]
    assert fk.column.table.name == "positions"
    assert fk.column.name == "id"


def test_portfolio_snapshot_columns():
    expected = {
        "id",
        "total_usd",
        "stablecoin_balance",
        "open_position_count",
        "realized_pnl_today",
        "peak_total_usd",
        "current_drawdown_pct",
        "snapshotted_at",
    }
    cols = {c.name for c in PortfolioSnapshot.__table__.c}
    assert cols == expected


# ---------------------------------------------------------------------------
# FK constraint enforcement
# ---------------------------------------------------------------------------


async def test_trade_fk_raises_on_invalid_position(session: AsyncSession):
    bad_trade = Trade(
        position_id=9999,  # no such position
        direction="open",
        asset="ETH",
        price=Decimal("3000.00000000"),
        size_usd=Decimal("500.00000000"),
        slippage_pct=Decimal("0.00100000"),
        tx_hash="paper_abc123",
        executed_at=datetime.now(tz=timezone.utc),
    )
    session.add(bad_trade)
    with pytest.raises(IntegrityError):
        await session.flush()


# ---------------------------------------------------------------------------
# Insert / roundtrip
# ---------------------------------------------------------------------------


async def test_agent_insert_and_query(session: AsyncSession):
    now = datetime.now(tz=timezone.utc)
    agent = Agent(
        agent_id=42,
        wallet_address="0xDEAD",
        registration_uri="ipfs://Qm123",
        registered_at=now,
    )
    session.add(agent)
    await session.flush()
    await session.refresh(agent)
    assert agent.id is not None
    assert agent.agent_id == 42


async def test_portfolio_snapshot_insert(session: AsyncSession):
    snap = PortfolioSnapshot(
        total_usd=Decimal("10500.00000000"),
        stablecoin_balance=Decimal("9500.00000000"),
        open_position_count=1,
        realized_pnl_today=Decimal("50.00000000"),
        peak_total_usd=Decimal("10500.00000000"),
        current_drawdown_pct=Decimal("0.00000000"),
        snapshotted_at=datetime.now(tz=timezone.utc),
    )
    session.add(snap)
    await session.flush()
    await session.refresh(snap)
    assert snap.id is not None


async def test_position_and_trade_roundtrip(session: AsyncSession):
    now = datetime.now(tz=timezone.utc)
    pos = Position(
        asset="SOL",
        status="open",
        action="LONG",
        entry_price=Decimal("143.21000000"),
        size_usd=Decimal("900.00000000"),
        size_pct=Decimal("0.09000000"),
        stop_loss=Decimal("141.85000000"),
        take_profit=Decimal("147.28000000"),
        strategy="trend_following",
        trigger_reason="clock",
        reasoning="ipfs://Qmabc",
        opened_at=now,
        tx_hash_open="paper_xyz",
    )
    session.add(pos)
    await session.flush()

    trade = Trade(
        position_id=pos.id,
        direction="open",
        asset="SOL",
        price=Decimal("143.21000000"),
        size_usd=Decimal("900.00000000"),
        slippage_pct=Decimal("0.00100000"),
        tx_hash="paper_xyz",
        executed_at=now,
    )
    session.add(trade)
    await session.flush()
    await session.refresh(trade)

    assert trade.id is not None
    assert trade.position_id == pos.id
