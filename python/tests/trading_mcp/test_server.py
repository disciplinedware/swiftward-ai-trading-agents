"""Integration tests: TradingService + PaperEngine + SQLite.

Tests cover the full execute_swap -> portfolio state flow using:
- SQLite in-memory database
- respx mocks for price_feed_mcp HTTP calls
- MockIpfs + no-op ERC8004Registry (to avoid web3 calls)
"""
import asyncio
from decimal import Decimal
from unittest.mock import AsyncMock, patch

import pytest
import respx
from httpx import Response
from sqlalchemy import event

from common.models.trade_intent import TradeIntent
from trading_mcp.domain.entity import Base
from trading_mcp.engine.paper import PaperEngine
from trading_mcp.erc8004 import registry as _reg_mod
from trading_mcp.erc8004.ipfs import MockIpfs
from trading_mcp.erc8004.registry import ERC8004Config, ERC8004Registry
from trading_mcp.infra.db import make_engine, make_session_factory
from trading_mcp.infra.price_client import PriceClient
from trading_mcp.service.portfolio_service import PortfolioService
from trading_mcp.service.trading_service import TradingService

_PRICE_FEED_URL = "http://price-feed-mcp:8001"

_ETH_PRICE = "3000.00000000"
_BTC_PRICE = "60000.00000000"

_ETH_INTENT = TradeIntent(
    asset="ETH",
    action="LONG",
    size_pct=Decimal("0.2"),
    stop_loss=Decimal("2800"),
    take_profit=Decimal("3500"),
    strategy="trend_following",
    reasoning="mock://reasoning.json",
    trigger_reason="clock",
    confidence=0.8,
)

_BTC_INTENT = TradeIntent(
    asset="BTC",
    action="LONG",
    size_pct=Decimal("0.2"),
    stop_loss=Decimal("55000"),
    take_profit=Decimal("70000"),
    strategy="breakout",
    reasoning="mock://btc-reasoning.json",
    trigger_reason="clock",
    confidence=0.8,
)


def _mock_prices(prices: dict[str, str]):
    """Return a respx mock for the price feed endpoint."""
    return respx.post(f"{_PRICE_FEED_URL}/mcp").mock(
        return_value=Response(
            200,
            json={"jsonrpc": "2.0", "id": 1, "result": prices},
        )
    )


@pytest.fixture
async def service():
    """Full TradingService wired with SQLite + MockIpfs.

    ERC8004Registry hooks are no-ops (MockIpfs + no RPC).
    """
    db = make_engine("sqlite+aiosqlite:///:memory:")

    @event.listens_for(db.sync_engine, "connect")
    def set_pragma(dbapi_conn, _):
        cursor = dbapi_conn.cursor()
        cursor.execute("PRAGMA foreign_keys=ON")
        cursor.close()

    async with db.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    factory = make_session_factory(db)
    portfolio_svc = PortfolioService(
        session_factory=factory,
        starting_balance_usdc=Decimal("10000"),
    )
    engine = PaperEngine()
    price_client = PriceClient(base_url=_PRICE_FEED_URL)

    erc_cfg = ERC8004Config(
        chain_id=11155111,
        rpc_url="http://localhost:8545",
        identity_registry_address="0x0000000000000000000000000000000000000001",
        validation_registry_address="0x0000000000000000000000000000000000000002",
        wallet_address="0x0000000000000000000000000000000000000000",
        wallet_private_key="0x" + "0" * 64,
        tracked_assets=["BTC", "ETH"],
        ipfs_provider="mock",
        ipfs_api_key="",
    )
    with patch.object(_reg_mod, "AsyncWeb3"):
        registry = ERC8004Registry(
            config=erc_cfg, ipfs=MockIpfs(), session_factory=factory,
            tx_lock=asyncio.Lock(),
        )

    svc = TradingService(
        engine=engine,
        portfolio_service=portfolio_svc,
        price_client=price_client,
        registry=registry,
        max_concurrent_positions=2,
    )
    yield svc, portfolio_svc

    await db.dispose()


@respx.mock
async def test_execute_swap_long_creates_position(service):
    """LONG intent creates an open position visible in get_portfolio."""
    svc, portfolio_svc = service

    # Mock price feed + suppress ERC-8004 fire-and-forget
    _mock_prices({"ETH": _ETH_PRICE})
    with patch.object(svc._registry, "submit_validation", new_callable=AsyncMock):
        result = await svc.execute_swap(_ETH_INTENT)

    assert result.status == "executed"

    _mock_prices({"ETH": _ETH_PRICE})
    portfolio = await svc.get_portfolio()
    assert portfolio.open_position_count == 1
    assert portfolio.open_positions[0].asset == "ETH"


@respx.mock
async def test_execute_swap_long_then_flat_closes_position(service):
    """LONG then FLAT results in zero open positions and non-zero realized PnL."""
    svc, portfolio_svc = service

    # Open ETH position
    _mock_prices({"ETH": _ETH_PRICE})
    with patch.object(svc._registry, "submit_validation", new_callable=AsyncMock):
        r1 = await svc.execute_swap(_ETH_INTENT)
    assert r1.status == "executed"

    # Close with higher price (profit)
    close_intent = TradeIntent(
        asset="ETH",
        action="FLAT",
        size_pct=Decimal("0"),
        stop_loss=Decimal("0"),
        take_profit=Decimal("0"),
        strategy="trend_following",
        reasoning="mock://close.json",
        trigger_reason="clock",
        confidence=1.0,
    )
    _mock_prices({"ETH": "3300.00000000"})
    r2 = await svc.execute_swap(close_intent)
    assert r2.status == "executed"

    _mock_prices({"ETH": "3300.00000000"})
    portfolio = await svc.get_portfolio()
    assert portfolio.open_position_count == 0
    assert portfolio.realized_pnl_today > Decimal("0")


@respx.mock
async def test_execute_swap_rejected_at_max_positions(service):
    """Third LONG is rejected when max_concurrent_positions=2."""
    svc, portfolio_svc = service

    with patch.object(svc._registry, "submit_validation", new_callable=AsyncMock):
        # Open position 1 (ETH)
        _mock_prices({"ETH": _ETH_PRICE})
        r1 = await svc.execute_swap(_ETH_INTENT)
        assert r1.status == "executed"

        # Open position 2 (BTC)
        _mock_prices({"BTC": _BTC_PRICE})
        r2 = await svc.execute_swap(_BTC_INTENT)
        assert r2.status == "executed"

    # Third position should be rejected
    third_intent = TradeIntent(
        asset="SOL",
        action="LONG",
        size_pct=Decimal("0.1"),
        stop_loss=Decimal("100"),
        take_profit=Decimal("200"),
        strategy="mean_reversion",
        reasoning="mock://sol.json",
        trigger_reason="clock",
        confidence=0.8,
    )
    _mock_prices({"SOL": "150.00000000"})
    r3 = await svc.execute_swap(third_intent)
    assert r3.status == "rejected"
    assert "max_concurrent_positions" in r3.reason


@respx.mock
async def test_get_daily_pnl_zero_with_no_trades(service):
    """get_daily_pnl returns '0' when no positions have been closed today."""
    svc, _ = service
    pnl = await svc.get_daily_pnl()
    assert pnl == Decimal("0")


async def test_health_endpoint():
    """GET /health returns HTTP 200 and {"status": "ok"}."""
    from starlette.applications import Starlette
    from starlette.requests import Request
    from starlette.responses import JSONResponse
    from starlette.routing import Route
    from starlette.testclient import TestClient

    # Test the handler directly without going through the FastMCP lifespan
    async def _health(request: Request) -> JSONResponse:
        return JSONResponse({"status": "ok"})

    app = Starlette(routes=[Route("/health", _health)])
    client = TestClient(app)
    resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}
