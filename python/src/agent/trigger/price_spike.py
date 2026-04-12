import asyncio
from decimal import Decimal

from agent.infra.price_feed_mcp import PriceFeedMCPClient
from agent.trigger.clock import ClockLoop
from agent.trigger.cooldown import CooldownGate
from common.config import AgentConfig
from common.log import get_logger

logger = get_logger(__name__)

_INTERVAL_SECONDS = 60


class PriceSpikeLoop:
    def __init__(
        self,
        *,
        price_feed: PriceFeedMCPClient,
        gate: CooldownGate,
        clock: ClockLoop,
        config: AgentConfig,
    ) -> None:
        self._price_feed = price_feed
        self._gate = gate
        self._clock = clock
        self._threshold = Decimal(str(config.trigger.price_spike_threshold_pct))
        self._tracked = config.assets.tracked

    async def run(self) -> None:
        """Poll for price spikes every 60 seconds."""
        while True:
            await self._check()
            await asyncio.sleep(_INTERVAL_SECONDS)

    async def _check(self) -> None:
        logger.debug("price_spike: poll start")

        try:
            changes = await self._price_feed.get_prices_change_only(self._tracked)
        except Exception as exc:
            logger.error("price_spike: price change fetch failed, skipping", error=str(exc))
            return

        spiking = [
            asset for asset in self._tracked
            if self._is_spike(changes.get(asset, {}))
        ]

        if not spiking:
            logger.debug("price_spike: no spikes detected")
            return

        logger.info("price_spike: spikes detected", assets=spiking)

        triggered = [a for a in spiking if self._gate.is_cooldown_open(a)]

        if not triggered:
            logger.info("price_spike: all spiking assets gated, skipping brain", assets=spiking)
            return

        logger.info("price_spike: triggering brain cycle", trigger_assets=triggered)
        await self._clock._run_once("price_spike")

    def _is_spike(self, windows: dict[str, Decimal]) -> bool:
        change_1m = abs(windows.get("1m", Decimal("0")))
        change_5m = abs(windows.get("5m", Decimal("0")))
        return change_1m >= self._threshold or change_5m >= self._threshold
