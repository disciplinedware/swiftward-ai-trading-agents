"""PaperEngine unit tests — pure fill-price logic, no DB."""
from decimal import Decimal

import pytest

from common.models.trade_intent import TradeIntent
from trading_mcp.engine.paper import PaperEngine


def _intent(**kwargs) -> TradeIntent:
    defaults = dict(
        asset="ETH",
        action="LONG",
        size_pct=0.09,
        stop_loss=1900.0,
        take_profit=2300.0,
        strategy="trend_following",
        reasoning="ipfs://Qm1",
        trigger_reason="clock",
        confidence=0.8,
    )
    defaults.update(kwargs)
    return TradeIntent(**defaults)


@pytest.fixture
def engine() -> PaperEngine:
    return PaperEngine()


@pytest.mark.parametrize(
    "price,amount_usd,expected_fill",
    [
        (Decimal("2000"), Decimal("900"), Decimal("2002.00000000")),
        (Decimal("100"),  Decimal("500"), Decimal("100.10000000")),
        (Decimal("50000"), Decimal("1000"), Decimal("50050.00000000")),
    ],
)
async def test_long_fill_price_includes_slippage(engine, price, amount_usd, expected_fill):
    result = await engine.execute_swap(_intent(action="LONG"), price, amount_usd)
    assert result.status == "executed"
    assert result.executed_price == expected_fill
    assert result.slippage_pct == Decimal("0.001")


async def test_long_size_usd_equals_amount_usd(engine):
    amount = Decimal("1234.56000000")
    result = await engine.execute_swap(_intent(), Decimal("2000"), amount)
    assert result.status == "executed"
    assert result.size_usd == amount


async def test_long_tx_hash_format(engine):
    result = await engine.execute_swap(_intent(), Decimal("3000"), Decimal("500"))
    assert result.status == "executed"
    assert result.tx_hash.startswith("paper_")
    assert len(result.tx_hash) > len("paper_")


async def test_flat_fill_price_includes_slippage(engine):
    amount = Decimal("800.00000000")
    result = await engine.execute_swap(_intent(action="FLAT"), Decimal("2200"), amount)
    assert result.status == "executed"
    assert result.executed_price == Decimal("2197.80000000")  # 2200 × (1 - 0.001)
    assert result.slippage_pct == Decimal("0.001")
    assert result.tx_hash.startswith("paper_")
    assert result.size_usd == amount
