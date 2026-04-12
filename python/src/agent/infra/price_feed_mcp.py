import asyncio
from decimal import Decimal

import httpx

from common.models.signal_bundle import PriceFeedData

from ._mcp_client import MCPClientMixin


class PriceFeedMCPClient(MCPClientMixin):
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=30.0)

    async def get_prices_latest(self, assets: list[str]) -> dict[str, Decimal]:
        if not assets:
            return {}
        body = await self._call("get_prices_latest", {"assets": assets})
        return {asset: Decimal(str(price)) for asset, price in body["result"].items()}

    async def get_prices_change_only(self, assets: list[str]) -> dict[str, dict[str, Decimal]]:
        if not assets:
            return {}
        body = await self._call("get_prices_change", {"assets": assets})
        return {
            asset: {window: Decimal(str(val)) for window, val in windows.items()}
            for asset, windows in body["result"].items()
        }

    async def get_prices(self, assets: list[str]) -> dict[str, PriceFeedData]:
        if not assets:
            return {}
        latest_body, changes_body, indicators_body = await asyncio.gather(
            self._call("get_prices_latest", {"assets": assets}),
            self._call("get_prices_change", {"assets": assets}),
            self._call("get_indicators", {"assets": assets}),
        )
        latest: dict[str, str] = latest_body["result"]
        changes: dict[str, dict[str, str]] = changes_body["result"]
        indicators: dict[str, dict] = indicators_body["result"]

        result: dict[str, PriceFeedData] = {}
        for asset in assets:
            chg = changes.get(asset, {})
            ind = indicators.get(asset, {})
            result[asset] = PriceFeedData(
                price=latest.get(asset, "0"),
                change_1m=chg.get("1m", "0"),
                change_5m=chg.get("5m", "0"),
                change_1h=chg.get("1h", "0"),
                change_4h=chg.get("4h", "0"),
                change_24h=chg.get("24h", "0"),
                rsi_14_15m=ind.get("rsi_14_15m", "0"),
                ema_20_15m=ind.get("ema_20_15m", "0"),
                ema_50_15m=ind.get("ema_50_15m", "0"),
                ema_50_1h=ind.get("ema_50_1h", "0"),
                ema_200_1h=ind.get("ema_200_1h", "0"),
                atr_14_15m=ind.get("atr_14_15m", "0"),
                bb_upper_15m=ind.get("bb_upper_15m", "0"),
                bb_mid_15m=ind.get("bb_mid_15m", "0"),
                bb_lower_15m=ind.get("bb_lower_15m", "0"),
                volume_ratio_15m=ind.get("volume_ratio_15m", "1"),
            )
        return result
