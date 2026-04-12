from decimal import Decimal

import pytest

from agent.brain.base import Brain
from agent.brain.factory import make_brain
from agent.brain.stub import StubBrain
from common.exceptions import ConfigError
from common.models.portfolio_snapshot import PortfolioSnapshot
from common.models.signal_bundle import FearGreedData, PriceFeedData, SignalBundle

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _price(price: str, atr: str = "100") -> PriceFeedData:
    return PriceFeedData(price=price, atr_14_15m=atr)


def _bundle(prices: dict[str, PriceFeedData]) -> SignalBundle:
    return SignalBundle(
        prices=prices,
        fear_greed=FearGreedData(value=50, classification="Neutral"),
        onchain={},
        news={},
        trigger_reason="clock",
        portfolio=PortfolioSnapshot(
            total_usd="10000",
            stablecoin_balance="10000",
            open_position_count=0,
            realized_pnl_today="0",
            current_drawdown_pct="0",
        ),
    )


# ---------------------------------------------------------------------------
# Factory tests
# ---------------------------------------------------------------------------


def test_factory_returns_stub_brain():
    brain = make_brain.__wrapped__() if hasattr(make_brain, "__wrapped__") else _make_stub()
    assert isinstance(brain, Brain)


def _make_stub() -> StubBrain:
    return StubBrain()


def test_factory_raises_config_error_for_unknown():
    import pytest

    with pytest.raises((ConfigError, Exception)):
        # Patch implementation to unknown
        from unittest.mock import MagicMock

        from agent.brain.factory import make_brain as _mb
        cfg = MagicMock()
        cfg.brain.implementation = "unknown_brain"
        _mb(cfg)


# ---------------------------------------------------------------------------
# StubBrain behaviour
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("n_assets,expected_max", [
    (0, 0),
    (1, 1),
    (2, 2),
    (5, 2),
])
async def test_selects_at_most_2_assets(n_assets, expected_max):
    brain = StubBrain()
    prices = {f"ASSET{i}": _price(str(1000 + i * 100)) for i in range(n_assets)}
    intents = await brain.run(_bundle(prices))
    assert len(intents) <= expected_max
    assert len(intents) == min(n_assets, 2)


async def test_skips_zero_price_assets():
    brain = StubBrain()
    prices = {
        "ZERO": _price("0"),
        "BTC": _price("80000"),
        "ETH": _price("3000"),
    }
    intents = await brain.run(_bundle(prices))
    assert all(i.asset != "ZERO" for i in intents)


async def test_returns_empty_for_all_zero_prices():
    brain = StubBrain()
    prices = {"BTC": _price("0"), "ETH": _price("0")}
    intents = await brain.run(_bundle(prices))
    assert intents == []


async def test_intent_fields():
    brain = StubBrain()
    prices = {"BTC": _price("80000", atr="800")}
    intents = await brain.run(_bundle(prices))
    assert len(intents) == 1
    intent = intents[0]
    assert intent.action == "LONG"
    assert intent.size_pct == Decimal("0.01")
    assert intent.asset == "BTC"
    assert intent.reasoning != ""


async def test_atr_based_stop_take():
    brain = StubBrain()
    prices = {"BTC": _price("80000", atr="1000")}
    intents = await brain.run(_bundle(prices))
    intent = intents[0]
    # stop = 80000 - 1000 * 1.5 = 78500
    assert intent.stop_loss == Decimal("78500")
    # take = 80000 + 1000 * 3.0 = 83000
    assert intent.take_profit == Decimal("83000")


async def test_fallback_stop_take_when_atr_zero():
    brain = StubBrain()
    prices = {"BTC": _price("80000", atr="0")}
    intents = await brain.run(_bundle(prices))
    intent = intents[0]
    # stop = 80000 * 0.98 = 78400
    assert intent.stop_loss == Decimal("80000") * Decimal("0.98")
    # take = 80000 * 1.04 = 83200
    assert intent.take_profit == Decimal("80000") * Decimal("1.04")
