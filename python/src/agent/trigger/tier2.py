import asyncio

from agent.infra.fear_greed_mcp import FearGreedMCPClient
from agent.infra.news_mcp import NewsMCPClient
from agent.trigger.clock import ClockLoop
from common.config import AgentConfig
from common.log import get_logger

logger = get_logger(__name__)

_INTERVAL_SECONDS = 5 * 60


class Tier2Loop:
    def __init__(
        self,
        *,
        fear_greed: FearGreedMCPClient,
        news: NewsMCPClient,
        clock: ClockLoop,
        config: AgentConfig,
    ) -> None:
        self._fear_greed = fear_greed
        self._news = news
        self._clock = clock
        self._fg_low = config.trigger.fear_greed_low
        self._fg_high = config.trigger.fear_greed_high
        self._tracked = config.assets.tracked

    async def run(self) -> None:
        """Poll tier-2 conditions every 5 minutes."""
        while True:
            await self._check()
            await asyncio.sleep(_INTERVAL_SECONDS)

    async def _check(self) -> None:
        logger.debug("tier2: poll start")

        try:
            fg, news_signals = await asyncio.gather(
                self._fear_greed.get_index(),
                self._news.get_signals(self._tracked),
            )
        except Exception as exc:
            logger.error("tier2: fetch failed, skipping cycle", error=str(exc))
            return

        fg_triggered = fg.value < self._fg_low or fg.value > self._fg_high
        news_triggered = any(d.macro_flag for d in news_signals.values())

        if not fg_triggered and not news_triggered:
            logger.debug("tier2: no conditions met")
            return

        reasons = []
        if fg_triggered:
            reasons.append(f"fear_greed={fg.value} (low={self._fg_low}, high={self._fg_high})")
        if news_triggered:
            reasons.append("macro_flag=True")

        logger.info("tier2: condition met, triggering brain cycle", reasons=reasons)
        trigger = "news" if news_triggered else "fear_greed"
        await self._clock._run_once(trigger)
