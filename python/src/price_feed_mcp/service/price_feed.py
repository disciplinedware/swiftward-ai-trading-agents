from decimal import Decimal
from typing import cast

from common.cache import RedisCache
from common.exceptions import MCPError
from common.log import get_logger
from price_feed_mcp.infra.binance import BinanceClient, asset_to_symbol
from price_feed_mcp.service.indicators import compute_indicators

logger = get_logger(__name__)

_CACHE_TTL = 30  # seconds

_ZERO_CHANGE = {"1m": "0", "5m": "0", "1h": "0", "4h": "0", "24h": "0"}


class PriceFeedService:
    def __init__(self, binance: BinanceClient, cache: RedisCache) -> None:
        self._binance = binance
        self._cache = cache

    async def get_prices_latest(self, assets: list[str]) -> dict[str, str]:
        result: dict[str, str] = {}
        for asset in assets:
            symbol = asset_to_symbol(asset)
            key = f"price_feed:{symbol}:ticker"
            cached = await self._cache.get(key)
            if cached is not None:
                result[asset] = cast(dict, cached)["price"]
                continue
            price: Decimal = await self._binance.get_ticker_price(symbol)
            price_str = str(price)
            await self._cache.set(key, {"price": price_str}, _CACHE_TTL)
            result[asset] = price_str
        return result

    async def get_prices_change(self, assets: list[str]) -> dict[str, dict[str, str]]:
        result: dict[str, dict[str, str]] = {}
        for asset in assets:
            symbol = asset_to_symbol(asset)
            try:
                klines_1m = await self._fetch_klines(symbol, "1m", 2)
                klines_5m = await self._fetch_klines(symbol, "5m", 2)
                klines_1h = await self._fetch_klines(symbol, "1h", 250)
                result[asset] = {
                    "1m": _pct_change(klines_1m),
                    "5m": _pct_change(klines_5m),
                    "1h": _pct_change(klines_1h, lookback=1),
                    "4h": _pct_change(klines_1h, lookback=4),
                    "24h": _pct_change(klines_1h, lookback=24),
                }
            except (MCPError, Exception) as exc:
                logger.warning(
                    "price change unavailable, returning zeros", asset=asset, error=str(exc)
                )
                result[asset] = _ZERO_CHANGE.copy()
        return result

    async def get_indicators(self, assets: list[str]) -> dict[str, dict]:
        result: dict[str, dict] = {}
        for asset in assets:
            symbol = asset_to_symbol(asset)
            try:
                klines_15m = await self._fetch_klines(symbol, "15m", 100)
                klines_1h = await self._fetch_klines(symbol, "1h", 250)
                result[asset] = compute_indicators(klines_15m, klines_1h)
            except (MCPError, Exception) as exc:
                logger.warning(
                    "indicators unavailable, returning zeros", asset=asset, error=str(exc)
                )
                result[asset] = {}
        return result

    async def _fetch_klines(self, symbol: str, interval: str, limit: int) -> list[list]:
        key = f"price_feed:{symbol}:{interval}"
        cached = await self._cache.get(key)
        if cached is not None:
            return cast(list[list], cached)
        klines = await self._binance.get_klines(symbol, interval, limit)
        await self._cache.set(key, klines, _CACHE_TTL)
        return klines


def _pct_change(klines: list[list], lookback: int = 1) -> str:
    close_now = Decimal(klines[-1][4])
    close_prev = Decimal(klines[-1 - lookback][4])
    if close_prev == 0:
        return "0"
    return str(round((close_now - close_prev) / close_prev * 100, 6))
