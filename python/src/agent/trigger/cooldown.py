import asyncio
from datetime import datetime, timedelta, timezone

from common.config import TradingConfig, TriggerConfig
from common.log import get_logger

logger = get_logger(__name__)


class CooldownGate:
    def __init__(self, trigger: TriggerConfig, trading: TradingConfig, mcp_client: object) -> None:
        self._cooldown = timedelta(minutes=trigger.cooldown_minutes)
        self._max_positions = trading.max_concurrent_positions
        self._mcp_client = mcp_client
        self._timestamps: dict[str, datetime] = {}
        self._lock = asyncio.Lock()

    def record_trade(self, asset: str) -> None:
        """Record that a trade was made for this asset, closing the gate."""
        self._timestamps[asset] = datetime.now(timezone.utc)

    async def is_allowed(self, asset: str) -> bool:
        """Return True if a trade on this asset is permitted right now."""
        async with self._lock:
            if not self.is_cooldown_open(asset):
                logger.info("cooldown gate: suppressed (cooldown active)", asset=asset)
                return False
            return await self._global_positions_ok()

    def is_cooldown_open(self, asset: str) -> bool:
        last = self._timestamps.get(asset)
        if last is None:
            return True
        return datetime.now(timezone.utc) - last >= self._cooldown

    async def _global_positions_ok(self) -> bool:
        try:
            portfolio = await self._mcp_client.get_portfolio()  # type: ignore[attr-defined]
            if portfolio.open_position_count >= self._max_positions:
                logger.info(
                    "cooldown gate: suppressed (max positions reached)",
                    open=portfolio.open_position_count,
                    max=self._max_positions,
                )
                return False
            return True
        except Exception as exc:
            logger.warning("cooldown gate: MCP error, suppressing trade", error=str(exc))
            return False
