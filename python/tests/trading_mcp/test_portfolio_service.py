"""Portfolio service tests using SQLite in-memory.

All financial arithmetic verified with exact Decimal values.
No live Postgres required.
"""
import asyncio
from datetime import datetime, timedelta, timezone
from decimal import Decimal

import pytest
from sqlalchemy import event, select

from common.models.trade_intent import TradeIntent
from trading_mcp.domain.entity import Base, Position
from trading_mcp.engine.interface import ExecutionResult
from trading_mcp.infra.db import make_engine, make_session_factory
from trading_mcp.service.portfolio_service import PortfolioService


@pytest.fixture
async def service():
    engine = make_engine("sqlite+aiosqlite:///:memory:")

    @event.listens_for(engine.sync_engine, "connect")
    def set_pragma(dbapi_conn, _):
        cursor = dbapi_conn.cursor()
        cursor.execute("PRAGMA foreign_keys=ON")
        cursor.close()

    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    factory = make_session_factory(engine)
    svc = PortfolioService(factory, starting_balance_usdc=Decimal("10000"))
    yield svc
    await engine.dispose()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _intent(asset: str = "ETH", size_pct: float = 0.1, **kwargs) -> TradeIntent:
    defaults = dict(
        asset=asset,
        action="LONG",
        size_pct=size_pct,
        stop_loss=1900.0,
        take_profit=2300.0,
        strategy="trend_following",
        reasoning="ipfs://Qm1",
        trigger_reason="clock",
        confidence=0.8,
    )
    defaults.update(kwargs)
    return TradeIntent(**defaults)


def _open_result(
    fill_price: Decimal = Decimal("2002.00000000"),
    size_usd: Decimal = Decimal("1000.00000000"),
    tx: str = "paper_abc",
) -> ExecutionResult:
    return ExecutionResult(
        status="executed",
        tx_hash=tx,
        executed_price=fill_price,
        slippage_pct=Decimal("0.001"),
        size_usd=size_usd,
        intent_hash=b"\x00" * 32,
    )


def _flat_result(
    fill_price: Decimal,
    size_usd: Decimal = Decimal("1000.00000000"),
    tx: str = "paper_xyz",
) -> ExecutionResult:
    return ExecutionResult(
        status="executed",
        tx_hash=tx,
        executed_price=fill_price,
        slippage_pct=Decimal("0"),
        size_usd=size_usd,
        intent_hash=b"\x00" * 32,
    )


# ---------------------------------------------------------------------------
# get_portfolio
# ---------------------------------------------------------------------------


async def test_get_portfolio_empty(service: PortfolioService):
    summary = await service.get_portfolio({})
    assert summary.total_usd == Decimal("10000")
    assert summary.open_position_count == 0
    assert summary.current_drawdown_pct == Decimal("0")
    assert summary.open_positions == []


async def test_get_portfolio_with_open_position(service: PortfolioService):
    await service.record_open(
        _intent("ETH", size_pct=0.1), _open_result(Decimal("2000.00000000"))
    )
    summary = await service.get_portfolio({"ETH": Decimal("2200")})
    assert summary.open_position_count == 1
    pv = summary.open_positions[0]
    # size_usd = 10000 * 0.1 = 1000; pnl = (2200-2000)/2000 * 1000 = 100
    assert pv.unrealized_pnl_usd == Decimal("100.00000000")
    assert pv.unrealized_pnl_pct == Decimal("0.10000000")
    # mark-to-market: 9000 stablecoin + (1000 + 100) = 10100
    assert summary.total_usd == Decimal("10100.00000000")


# ---------------------------------------------------------------------------
# get_positions — unrealized PnL arithmetic
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "entry,current,size_usd,exp_pnl_usd,exp_pnl_pct",
    [
        # entry=200, size=1000, current=220 → pnl=100, pct=0.10
        (Decimal("200"), Decimal("220"), Decimal("1000"),
         Decimal("100.00000000"), Decimal("0.10000000")),
        # entry=100, size=500, current=80 → pnl=-100, pct=-0.20
        (Decimal("100"), Decimal("80"), Decimal("500"),
         Decimal("-100.00000000"), Decimal("-0.20000000")),
        # entry=50, size=200, current=50 → pnl=0
        (Decimal("50"), Decimal("50"), Decimal("200"),
         Decimal("0.00000000"), Decimal("0.00000000")),
    ],
)
async def test_get_positions_unrealized_pnl(
    service: PortfolioService,
    entry: Decimal,
    current: Decimal,
    size_usd: Decimal,
    exp_pnl_usd: Decimal,
    exp_pnl_pct: Decimal,
):
    await service.record_open(
        _intent("BTC"),
        _open_result(fill_price=entry, size_usd=size_usd),
    )
    views = await service.get_positions({"BTC": current})
    assert len(views) == 1
    assert views[0].unrealized_pnl_usd == exp_pnl_usd
    assert views[0].unrealized_pnl_pct == exp_pnl_pct


async def test_get_positions_empty(service: PortfolioService):
    views = await service.get_positions({})
    assert views == []


# ---------------------------------------------------------------------------
# get_position — found / not found
# ---------------------------------------------------------------------------


async def test_get_position_found(service: PortfolioService):
    await service.record_open(
        _intent("SOL", size_pct=0.05), _open_result(Decimal("150.00000000"))
    )
    view = await service.get_position("SOL", Decimal("160"))
    assert view is not None
    assert view.asset == "SOL"


async def test_get_position_not_found(service: PortfolioService):
    result = await service.get_position("BTC", Decimal("50000"))
    assert result is None


# ---------------------------------------------------------------------------
# record_open
# ---------------------------------------------------------------------------


async def test_record_open_persists_position(service: PortfolioService):
    await service.record_open(_intent("ETH"), _open_result())
    views = await service.get_positions({"ETH": Decimal("2000")})
    assert len(views) == 1
    assert views[0].asset == "ETH"


async def test_record_open_always_writes_regardless_of_count(service: PortfolioService):
    # Even if we're "over limit" from a service perspective, record_open always writes
    await service.record_open(_intent("ETH"), _open_result())
    await service.record_open(_intent("BTC"), _open_result())
    await service.record_open(_intent("SOL"), _open_result())
    views = await service.get_positions({
        "ETH": Decimal("2000"), "BTC": Decimal("50000"), "SOL": Decimal("100"),
    })
    assert len(views) == 3


async def test_record_open_stores_result_size_usd(service: PortfolioService):
    await service.record_open(_intent("ETH"), _open_result(size_usd=Decimal("2000.00000000")))
    views = await service.get_positions({"ETH": Decimal("2002")})
    assert views[0].size_usd == Decimal("2000.00000000")




# ---------------------------------------------------------------------------
# record_close
# ---------------------------------------------------------------------------


async def test_record_close_computes_pnl(service: PortfolioService):
    # Open: fill=2000, size_usd = 10000 * 0.1 = 1000
    await service.record_open(
        _intent("ETH", size_pct=0.1), _open_result(Decimal("2000.00000000"))
    )
    # Close at 2200 → pnl = (2200-2000)/2000 * 1000 = 100
    closed = await service.record_close("ETH", _flat_result(Decimal("2200.00000000")))
    assert closed is True

    views = await service.get_positions({"ETH": Decimal("2200")})
    assert views == []  # no open positions

    pnl = await service.get_daily_pnl()
    assert pnl == Decimal("100.00000000")


async def test_record_close_no_op_when_no_open_position(service: PortfolioService):
    closed = await service.record_close("BTC", _flat_result(Decimal("50000")))
    assert closed is False
    pnl = await service.get_daily_pnl()
    assert pnl == Decimal("0")


async def test_record_close_tx_hash_stored(service: PortfolioService):
    await service.record_open(_intent("ETH"), _open_result())
    await service.record_close("ETH", _flat_result(Decimal("2100"), tx="paper_close_xyz"))

    async with service._sf() as session:
        pos = (await session.execute(
            select(Position).where(Position.asset == "ETH")
        )).scalar_one()
    assert pos.tx_hash_close == "paper_close_xyz"
    assert pos.status == "closed"


# ---------------------------------------------------------------------------
# can_open_position
# ---------------------------------------------------------------------------


async def test_can_open_position_true_when_empty(service: PortfolioService):
    assert await service.can_open_position(2) is True


async def test_can_open_position_false_when_at_limit(service: PortfolioService):
    await service.record_open(_intent("ETH"), _open_result())
    assert await service.can_open_position(1) is False


# ---------------------------------------------------------------------------
# get_daily_pnl
# ---------------------------------------------------------------------------


async def test_get_daily_pnl_sums_todays_closed(service: PortfolioService):
    # Open ETH: size_usd=1000, fill=2000
    await service.record_open(_intent("ETH"), _open_result(Decimal("2000")))
    # Open BTC: size_usd=1000, fill=50000
    await service.record_open(_intent("BTC"), _open_result(Decimal("50000")))
    # Close ETH at 2100: pnl = (2100-2000)/2000 * 1000 = 50
    await service.record_close("ETH", _flat_result(Decimal("2100"), size_usd=Decimal("1000")))
    # Close BTC at 49000: pnl = (49000-50000)/50000 * 1000 = -20
    await service.record_close("BTC", _flat_result(Decimal("49000"), size_usd=Decimal("1000")))

    pnl = await service.get_daily_pnl()
    assert pnl == Decimal("30.00000000")  # 50 + (-20) = 30


async def test_get_daily_pnl_excludes_yesterday(service: PortfolioService):
    await service.record_open(
        _intent("SOL", size_pct=0.1), _open_result(Decimal("150"))
    )
    await service.record_close("SOL", _flat_result(Decimal("160")))

    # Backdate closed_at to yesterday
    async with service._sf() as session:
        async with session.begin():
            pos = (await session.execute(
                select(Position).where(Position.asset == "SOL")
            )).scalar_one()
            pos.closed_at = datetime.now(tz=timezone.utc) - timedelta(days=1)

    pnl = await service.get_daily_pnl()
    assert pnl == Decimal("0")


# ---------------------------------------------------------------------------
# Concurrent write safety
# ---------------------------------------------------------------------------


async def test_concurrent_record_open(service: PortfolioService):
    async def _open(asset: str):
        await service.record_open(_intent(asset), _open_result())

    await asyncio.gather(_open("ETH"), _open("BTC"))

    views = await service.get_positions({"ETH": Decimal("2000"), "BTC": Decimal("50000")})
    assets = {v.asset for v in views}
    assert assets == {"ETH", "BTC"}
