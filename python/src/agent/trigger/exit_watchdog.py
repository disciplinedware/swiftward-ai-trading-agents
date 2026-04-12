import asyncio
from decimal import Decimal

from agent.infra.price_feed_mcp import PriceFeedMCPClient
from agent.infra.trading_client import TradingClient
from agent.trigger.cooldown import CooldownGate
from common.log import get_logger
from common.models.portfolio_snapshot import OpenPositionView
from common.models.trade_intent import TradeIntent

logger = get_logger(__name__)

_INTERVAL_SECONDS = 2 * 60


class ExitWatchdog:
    def __init__(
        self,
        *,
        trading: TradingClient,
        price_feed: PriceFeedMCPClient,
        gate: CooldownGate,
    ) -> None:
        self._trading = trading
        self._price_feed = price_feed
        self._gate = gate

    async def run(self) -> None:
        """Poll open positions every 2 minutes and fire FLAT on breach."""
        while True:
            await self._check()
            await asyncio.sleep(_INTERVAL_SECONDS)

    async def _check(self) -> None:
        logger.debug("exit_watchdog: cycle start")

        try:
            portfolio = await self._trading.get_portfolio()
        except Exception as exc:
            logger.error("exit_watchdog: portfolio fetch failed, skipping cycle", error=str(exc))
            return

        positions = portfolio.open_positions
        if not positions:
            logger.debug("exit_watchdog: no open positions, skipping cycle")
            return

        assets = [p.asset for p in positions]
        logger.debug("exit_watchdog: checking positions", assets=assets, count=len(assets))

        try:
            prices = await self._price_feed.get_prices_latest(assets)
        except Exception as exc:
            logger.error("exit_watchdog: price fetch failed, skipping cycle", error=str(exc))
            return

        for pos in positions:
            price = prices.get(pos.asset)
            if price is None:
                logger.warning("exit_watchdog: no price for asset, skipping", asset=pos.asset)
                continue
            logger.debug(
                "exit_watchdog: price check",
                asset=pos.asset,
                price=str(price),
                stop_loss=str(pos.stop_loss),
                take_profit=str(pos.take_profit),
            )
            try:
                await self._maybe_exit(pos, price)
            except Exception as exc:
                logger.error(
                    "exit_watchdog: error processing position",
                    asset=pos.asset,
                    error=str(exc),
                )

        logger.debug("exit_watchdog: cycle complete")

    async def _maybe_exit(self, pos: OpenPositionView, price: Decimal) -> None:
        if price <= pos.stop_loss:
            trigger_reason = "stop_loss"
            reasoning = (
                f"stop_loss triggered: price {price} <= stop_loss {pos.stop_loss}"
            )
        elif price >= pos.take_profit:
            trigger_reason = "take_profit"
            reasoning = (
                f"take_profit triggered: price {price} >= take_profit {pos.take_profit}"
            )
        else:
            return

        logger.info(
            "exit_watchdog: breach detected, firing FLAT",
            asset=pos.asset,
            price=str(price),
            trigger_reason=trigger_reason,
        )

        intent = TradeIntent(
            asset=pos.asset,
            action="FLAT",
            size_pct=Decimal("0"),
            strategy=pos.strategy,  # type: ignore[arg-type]
            trigger_reason=trigger_reason,  # type: ignore[arg-type]
            reasoning=reasoning,
            confidence=1.0,
        )

        try:
            result = await self._trading.execute_swap(intent)
            logger.info(
                "exit_watchdog: FLAT submitted",
                asset=pos.asset,
                trigger_reason=trigger_reason,
                status=result.get("status"),
                tx_hash=result.get("tx_hash"),
            )
            self._gate.record_trade(pos.asset)
        except Exception as exc:
            logger.error(
                "exit_watchdog: execute_swap failed",
                asset=pos.asset,
                trigger_reason=trigger_reason,
                error=str(exc),
            )
