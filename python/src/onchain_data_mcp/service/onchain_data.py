from datetime import UTC, datetime
from decimal import Decimal, InvalidOperation
from typing import cast

from common.cache import RedisCache
from common.log import get_logger
from onchain_data_mcp.infra.binance_futures import BinanceFuturesClient, asset_to_symbol

logger = get_logger(__name__)

_CACHE_TTL = 60 * 5  # 5 minutes

# Funding settles 3× per day (every 8h). Annualize: rate × 3 × 365 × 100
_FUNDING_ANNUALIZE = Decimal("3") * Decimal("365") * Decimal("100")


class OnchainDataService:
    def __init__(self, client: BinanceFuturesClient, cache: RedisCache) -> None:
        self._client = client
        self._cache = cache

    async def get_funding_rate(self, assets: list[str]) -> dict[str, dict]:
        result: dict[str, dict] = {}
        for asset in assets:
            key = f"onchain:funding:{asset}"
            cached = await self._cache.get(key)
            if cached is not None:
                result[asset] = cast(dict, cached)
                continue
            symbol = asset_to_symbol(asset)
            entry = _parse_funding(await self._client.get_premium_index(symbol))
            await self._cache.set(key, entry, _CACHE_TTL)
            result[asset] = entry
        return result

    async def get_open_interest(self, assets: list[str]) -> dict[str, dict]:
        result: dict[str, dict] = {}
        for asset in assets:
            key = f"onchain:oi:{asset}"
            cached = await self._cache.get(key)
            if cached is not None:
                result[asset] = cast(dict, cached)
                continue
            symbol = asset_to_symbol(asset)
            entry = _parse_open_interest(await self._client.get_open_interest_hist(symbol, "1d", 2))
            await self._cache.set(key, entry, _CACHE_TTL)
            result[asset] = entry
        return result

    async def get_liquidations(self, assets: list[str]) -> dict[str, dict]:
        result: dict[str, dict] = {}
        now_ms = _now_ms()
        start_ms = now_ms - 15 * 60 * 1000
        for asset in assets:
            key = f"onchain:liq:{asset}"
            cached = await self._cache.get(key)
            if cached is not None:
                result[asset] = cast(dict, cached)
                continue
            symbol = asset_to_symbol(asset)
            orders = await self._client.get_force_orders(symbol, start_ms, now_ms)
            entry = _aggregate_liquidations(orders)
            await self._cache.set(key, entry, _CACHE_TTL)
            result[asset] = entry
        return result

    async def get_netflow(self) -> dict[str, dict]:
        # TODO: Replace with CryptoQuant GET /v1/{btc,eth}/exchange-flows/netflow-total
        #       when a paid plan is available. The CryptoQuant free tier only covers
        #       Price OHLCV data — exchange netflow requires a paid subscription.
        return {
            "BTC": {"direction": "neutral", "note": "netflow data unavailable"},
            "ETH": {"direction": "neutral", "note": "netflow data unavailable"},
        }


# ---------------------------------------------------------------------------
# Pure helpers
# ---------------------------------------------------------------------------


def _parse_funding(raw: dict) -> dict:
    rate = Decimal(str(raw["lastFundingRate"]))
    annualized = rate * _FUNDING_ANNUALIZE
    next_funding_ms = int(raw["nextFundingTime"])
    next_funding_iso = datetime.fromtimestamp(next_funding_ms / 1000, tz=UTC).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )
    return {
        "funding_rate": str(rate.normalize()),
        "annualized_pct": str(round(annualized, 2).normalize()),
        "next_funding_time": next_funding_iso,
    }


def _parse_open_interest(snapshots: list[dict]) -> dict:
    if len(snapshots) < 2:
        latest_val = (
            Decimal(str(snapshots[-1]["sumOpenInterestValue"])) if snapshots else Decimal("0")
        )
        return {"oi_usd": str(latest_val), "change_pct_24h": "0"}

    prev = Decimal(str(snapshots[0]["sumOpenInterestValue"]))
    latest = Decimal(str(snapshots[-1]["sumOpenInterestValue"]))

    if prev == 0:
        change_pct = Decimal("0")
    else:
        change_pct = (latest - prev) / prev * Decimal("100")

    return {
        "oi_usd": str(latest),
        "change_pct_24h": str(round(change_pct, 2)),
    }


def _aggregate_liquidations(orders: list[dict]) -> dict:
    long_liq = Decimal("0")   # side=SELL → long position liquidated
    short_liq = Decimal("0")  # side=BUY  → short position liquidated

    for order in orders:
        try:
            usd = Decimal(str(order["avgPrice"])) * Decimal(str(order["executedQty"]))
        except (KeyError, InvalidOperation):
            continue
        if order.get("side") == "SELL":
            long_liq += usd
        else:
            short_liq += usd

    total = long_liq + short_liq
    return {
        "liquidated_usd_15m": str(round(total, 2)),
        "long_liquidated_usd": str(round(long_liq, 2)),
        "short_liquidated_usd": str(round(short_liq, 2)),
    }


def _now_ms() -> int:
    return int(datetime.now(UTC).timestamp() * 1000)
