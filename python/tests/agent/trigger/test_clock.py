from decimal import Decimal
from unittest.mock import AsyncMock, MagicMock

import pytest

from agent.trigger.clock import ClockLoop
from common.models.portfolio_snapshot import PortfolioSnapshot
from common.models.signal_bundle import FearGreedData, PriceFeedData
from common.models.trade_intent import TradeIntent


def _portfolio(count: int = 0) -> PortfolioSnapshot:
    return PortfolioSnapshot(
        total_usd="10000",
        stablecoin_balance="10000",
        open_position_count=count,
        realized_pnl_today="0",
        current_drawdown_pct="0",
    )


def _intent(asset: str, action: str = "LONG") -> TradeIntent:
    return TradeIntent(
        asset=asset,
        action=action,
        size_pct=Decimal("0.1"),
        stop_loss=Decimal("0") if action == "FLAT" else Decimal("90"),
        take_profit=Decimal("0") if action == "FLAT" else Decimal("110"),
        strategy="trend_following",
        reasoning="stub://",
        trigger_reason="clock",
        confidence=0.8,
    )


def _make_loop(
    tracked: list[str] | None = None,
    max_positions: int = 2,
    open_positions: int = 0,
    brain_intents: list[TradeIntent] | None = None,
    cooldown_open: bool = True,
) -> tuple[ClockLoop, AsyncMock, AsyncMock, AsyncMock, MagicMock]:
    """Build a ClockLoop with mocked dependencies.

    Returns (loop, trading, price_feed, brain, gate).
    """
    config = MagicMock()
    config.assets.tracked = tracked or ["BTC", "ETH"]
    config.trading.max_concurrent_positions = max_positions

    trading = AsyncMock()
    trading.get_portfolio.return_value = _portfolio(open_positions)
    trading.execute_swap.return_value = {
        "status": "executed", "tx_hash": "paper_123",
        "executed_price": "100", "slippage_pct": "0.001", "reason": None,
    }

    price_feed = AsyncMock()
    price_feed.get_prices.return_value = {
        a: PriceFeedData() for a in (tracked or ["BTC", "ETH"])
    }

    fear_greed = AsyncMock()
    fear_greed.get_index.return_value = FearGreedData()

    onchain = AsyncMock()
    onchain.get_all.return_value = {}

    news = AsyncMock()
    news.get_signals.return_value = {}

    brain = AsyncMock()
    brain.run.return_value = brain_intents or []

    gate = MagicMock()
    gate.is_cooldown_open.return_value = cooldown_open
    gate.record_trade = MagicMock()

    loop = ClockLoop(
        trading=trading,
        price_feed=price_feed,
        fear_greed=fear_greed,
        onchain=onchain,
        news=news,
        brain=brain,
        gate=gate,
        config=config,
    )
    return loop, trading, price_feed, brain, gate


@pytest.mark.parametrize("name,btc_open,eth_open,expected_assets", [
    ("all open → all passed", False, False, ["BTC", "ETH"]),
    ("BTC on cooldown → only ETH", True, False, ["ETH"]),
    ("both on cooldown → empty list", True, True, []),
])
async def test_cooldown_pre_filter(name, btc_open, eth_open, expected_assets):
    loop, _, price_feed, _, gate = _make_loop()
    gate.is_cooldown_open.side_effect = lambda a: not (
        (a == "BTC" and btc_open) or (a == "ETH" and eth_open)
    )
    await loop._run_once()
    price_feed.get_prices.assert_called_once_with(expected_assets)


async def test_portfolio_fetch_failure_skips_cycle():
    loop, trading, _, brain, _ = _make_loop()
    trading.get_portfolio.side_effect = Exception("connection refused")
    await loop._run_once()
    brain.run.assert_not_called()


async def test_signal_gather_failure_skips_cycle():
    loop, trading, price_feed, brain, _ = _make_loop()
    # First get_portfolio (cap check) succeeds; second (inside gather) fails
    trading.get_portfolio.side_effect = [_portfolio(0), Exception("timeout")]
    await loop._run_once()
    brain.run.assert_not_called()


async def test_position_cap_skips_cycle():
    loop, trading, price_feed, brain, _ = _make_loop(max_positions=2, open_positions=2)
    trading.get_portfolio.return_value = _portfolio(2)
    await loop._run_once()
    brain.run.assert_not_called()
    price_feed.get_prices.assert_not_called()


async def test_flat_submitted_before_long():
    long_intent = _intent("BTC", "LONG")
    flat_intent = _intent("ETH", "FLAT")
    loop, trading, _, brain, _ = _make_loop(brain_intents=[long_intent, flat_intent])
    await loop._run_once()

    calls = trading.execute_swap.call_args_list
    assert len(calls) == 2
    assert calls[0].args[0].action == "FLAT"
    assert calls[1].args[0].action == "LONG"


async def test_record_trade_called_on_success():
    intent = _intent("BTC", "LONG")
    loop, _, _, _, gate = _make_loop(brain_intents=[intent])
    await loop._run_once()
    gate.record_trade.assert_called_once_with("BTC")


async def test_record_trade_not_called_on_execute_swap_failure():
    intent = _intent("BTC", "LONG")
    loop, trading, _, _, gate = _make_loop(brain_intents=[intent])
    trading.execute_swap.side_effect = Exception("swap rejected")
    await loop._run_once()
    gate.record_trade.assert_not_called()
