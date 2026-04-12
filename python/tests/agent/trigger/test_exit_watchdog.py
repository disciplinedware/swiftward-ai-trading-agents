from decimal import Decimal
from unittest.mock import AsyncMock, MagicMock

import pytest

from agent.trigger.exit_watchdog import ExitWatchdog
from common.models.portfolio_snapshot import OpenPositionView, PortfolioSnapshot


def _portfolio(*positions: OpenPositionView) -> PortfolioSnapshot:
    return PortfolioSnapshot(
        total_usd="10000",
        stablecoin_balance="10000",
        open_position_count=len(positions),
        realized_pnl_today="0",
        current_drawdown_pct="0",
        open_positions=list(positions),
    )


def _pos(
    asset: str = "BTC",
    stop_loss: str = "90000",
    take_profit: str = "110000",
    strategy: str = "trend_following",
) -> OpenPositionView:
    return OpenPositionView(
        asset=asset,
        entry_price="100000",
        stop_loss=stop_loss,
        take_profit=take_profit,
        size_pct="0.1",
        strategy=strategy,
    )


def _make_watchdog(
    portfolio: PortfolioSnapshot | None = None,
    prices: dict[str, str] | None = None,
) -> tuple[ExitWatchdog, AsyncMock, AsyncMock, MagicMock]:
    trading = AsyncMock()
    trading.get_portfolio.return_value = portfolio or _portfolio()
    trading.execute_swap.return_value = {"status": "ok", "tx_hash": "paper_abc"}

    price_feed = AsyncMock()
    price_feed.get_prices_latest.return_value = {
        k: Decimal(v) for k, v in (prices or {}).items()
    }

    gate = MagicMock()

    watchdog = ExitWatchdog(trading=trading, price_feed=price_feed, gate=gate)
    return watchdog, trading, price_feed, gate


@pytest.mark.parametrize(
    "price, stop_loss, take_profit, expect_exit, trigger_reason",
    [
        # stop-loss breach (price at stop_loss)
        ("89000", "90000", "110000", True, "stop_loss"),
        # stop-loss breach (price below stop_loss)
        ("85000", "90000", "110000", True, "stop_loss"),
        # take-profit breach (price at take_profit)
        ("110000", "90000", "110000", True, "take_profit"),
        # take-profit breach (price above take_profit)
        ("115000", "90000", "110000", True, "take_profit"),
        # no breach
        ("100000", "90000", "110000", False, None),
        # price strictly between levels
        ("95000", "90000", "110000", False, None),
    ],
)
async def test_breach_detection(
    price: str,
    stop_loss: str,
    take_profit: str,
    expect_exit: bool,
    trigger_reason: str | None,
) -> None:
    pos = _pos(asset="BTC", stop_loss=stop_loss, take_profit=take_profit)
    watchdog, trading, _, gate = _make_watchdog(
        portfolio=_portfolio(pos),
        prices={"BTC": price},
    )

    await watchdog._check()

    if expect_exit:
        trading.execute_swap.assert_awaited_once()
        intent = trading.execute_swap.call_args[0][0]
        assert intent.action == "FLAT"
        assert intent.asset == "BTC"
        assert intent.trigger_reason == trigger_reason
        assert str(price) in intent.reasoning
        gate.record_trade.assert_called_once_with("BTC")
    else:
        trading.execute_swap.assert_not_awaited()
        gate.record_trade.assert_not_called()


async def test_no_positions_skips_price_fetch() -> None:
    watchdog, trading, price_feed, gate = _make_watchdog(portfolio=_portfolio())

    await watchdog._check()

    price_feed.get_prices_latest.assert_not_awaited()
    trading.execute_swap.assert_not_awaited()


async def test_multiple_positions_concurrent_price_fetch() -> None:
    btc = _pos(asset="BTC", stop_loss="90000", take_profit="110000")
    eth = _pos(asset="ETH", stop_loss="2000", take_profit="4000")
    watchdog, trading, price_feed, gate = _make_watchdog(
        portfolio=_portfolio(btc, eth),
        prices={"BTC": "100000", "ETH": "3000"},
    )

    await watchdog._check()

    # Single batched call with both assets
    price_feed.get_prices_latest.assert_awaited_once()
    assets_arg = price_feed.get_prices_latest.call_args[0][0]
    assert set(assets_arg) == {"BTC", "ETH"}
    trading.execute_swap.assert_not_awaited()


async def test_portfolio_error_skips_cycle() -> None:
    watchdog, trading, price_feed, gate = _make_watchdog()
    trading.get_portfolio.side_effect = RuntimeError("timeout")

    await watchdog._check()

    price_feed.get_prices_latest.assert_not_awaited()
    trading.execute_swap.assert_not_awaited()
    gate.record_trade.assert_not_called()


async def test_price_fetch_error_skips_cycle() -> None:
    pos = _pos(asset="BTC")
    watchdog, trading, price_feed, gate = _make_watchdog(portfolio=_portfolio(pos))
    price_feed.get_prices_latest.side_effect = RuntimeError("timeout")

    await watchdog._check()

    trading.execute_swap.assert_not_awaited()
    gate.record_trade.assert_not_called()


async def test_gate_check_not_called() -> None:
    """ExitWatchdog bypasses the cooldown gate check entirely."""
    pos = _pos(asset="BTC", stop_loss="90000", take_profit="110000")
    watchdog, _, _, gate = _make_watchdog(
        portfolio=_portfolio(pos),
        prices={"BTC": "85000"},
    )

    await watchdog._check()

    gate.is_allowed.assert_not_called()
    gate.is_cooldown_open.assert_not_called()


async def test_strategy_inherited_from_position() -> None:
    pos = _pos(asset="BTC", stop_loss="90000", take_profit="110000", strategy="breakout")
    watchdog, trading, _, _ = _make_watchdog(
        portfolio=_portfolio(pos),
        prices={"BTC": "85000"},
    )

    await watchdog._check()

    intent = trading.execute_swap.call_args[0][0]
    assert intent.strategy == "breakout"


async def test_execute_swap_error_does_not_crash() -> None:
    pos = _pos(asset="BTC", stop_loss="90000", take_profit="110000")
    watchdog, trading, _, gate = _make_watchdog(
        portfolio=_portfolio(pos),
        prices={"BTC": "85000"},
    )
    trading.execute_swap.side_effect = RuntimeError("network error")

    await watchdog._check()  # must not raise

    gate.record_trade.assert_not_called()
