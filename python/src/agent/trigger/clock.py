import asyncio

from agent.brain.base import Brain
from agent.infra.fear_greed_mcp import FearGreedMCPClient
from agent.infra.news_mcp import NewsMCPClient
from agent.infra.onchain_mcp import OnchainMCPClient
from agent.infra.price_feed_mcp import PriceFeedMCPClient
from agent.infra.trading_client import TradingClient
from agent.trigger.cooldown import CooldownGate
from common.config import AgentConfig
from common.log import get_logger
from common.models.signal_bundle import SignalBundle
from common.models.trade_intent import TriggerReason

logger = get_logger(__name__)

_INTERVAL_SECONDS = 15 * 60


class ClockLoop:
    def __init__(
        self,
        *,
        trading: TradingClient,
        price_feed: PriceFeedMCPClient,
        fear_greed: FearGreedMCPClient,
        onchain: OnchainMCPClient,
        news: NewsMCPClient,
        brain: Brain,
        gate: CooldownGate,
        config: AgentConfig,
    ) -> None:
        self._trading = trading
        self._price_feed = price_feed
        self._fear_greed = fear_greed
        self._onchain = onchain
        self._news = news
        self._brain = brain
        self._gate = gate
        self._config = config
        self._cycle_lock = asyncio.Lock()

    async def run(self) -> None:
        """Run the clock loop: fire immediately, then every 15 minutes."""
        while True:
            await self._run_once("clock")
            await asyncio.sleep(_INTERVAL_SECONDS)

    async def _run_once(self, trigger_reason: TriggerReason = "clock") -> None:
        if self._cycle_lock.locked():
            logger.info(
                "clock: skipping cycle, another is already running",
                trigger=trigger_reason,
            )
            return
        async with self._cycle_lock:
            await self._cycle(trigger_reason)

    async def _cycle(self, trigger_reason: TriggerReason) -> None:
        logger.info("clock: cycle start", trigger=trigger_reason)

        # Early position cap check (cheap portfolio fetch, no price computation)
        try:
            early_portfolio = await self._trading.get_portfolio()
        except Exception as exc:
            logger.error("clock: portfolio fetch failed, skipping cycle", error=str(exc))
            return

        max_pos = self._config.trading.max_concurrent_positions
        if early_portfolio.open_position_count >= max_pos:
            logger.info(
                "clock: position cap reached, skipping cycle",
                open=early_portfolio.open_position_count,
                max=max_pos,
            )
            return

        # Pre-filter assets by cooldown gate (sync, no I/O)
        allowed = [
            a for a in self._config.assets.tracked
            if self._gate.is_cooldown_open(a)
        ]
        logger.info("clock: allowed assets", assets=allowed)

        # Gather signals for allowed assets only
        try:
            bundle = await self._gather_signals(allowed, trigger_reason)
        except Exception as exc:
            logger.error("clock: signal gather failed, skipping cycle", error=str(exc))
            return

        # Run brain
        try:
            intents = await self._brain.run(bundle)
        except Exception as exc:
            logger.error("clock: brain failed, skipping cycle", error=str(exc))
            return

        # FLAT intents first, then LONG
        intents.sort(key=lambda i: 0 if i.action == "FLAT" else 1)

        # Submit intents
        for intent in intents:
            try:
                result = await self._trading.execute_swap(intent)
                logger.info(
                    "clock: intent submitted",
                    asset=intent.asset,
                    action=intent.action,
                    status=result.get("status"),
                    tx_hash=result.get("tx_hash"),
                )
                self._gate.record_trade(intent.asset)
            except Exception as exc:
                logger.error(
                    "clock: execute_swap failed",
                    asset=intent.asset,
                    action=intent.action,
                    error=str(exc),
                )

        # Post session checkpoint
        try:
            summary = f"{trigger_reason}: analyzed {len(allowed)} assets, {len(intents)} trades"
            await self._trading.end_cycle(summary)
        except Exception as exc:
            logger.warning("end_cycle failed", error=str(exc))

        logger.info("clock: cycle complete", submitted=len(intents))

    async def _gather_signals(
        self, allowed_assets: list[str], trigger_reason: TriggerReason
    ) -> SignalBundle:
        portfolio, prices, fear_greed, onchain, news = await asyncio.gather(
            self._trading.get_portfolio(),
            self._price_feed.get_prices(allowed_assets),
            self._fear_greed.get_index(),
            self._onchain.get_all(allowed_assets),
            self._news.get_signals(allowed_assets),
        )
        return SignalBundle(
            prices=prices,
            fear_greed=fear_greed,
            onchain=onchain,
            news=news,
            portfolio=portfolio,
            trigger_reason=trigger_reason,
        )
