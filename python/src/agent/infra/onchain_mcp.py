import asyncio

import httpx

from common.models.signal_bundle import OnchainData

from ._mcp_client import MCPClientMixin


class OnchainMCPClient(MCPClientMixin):
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=30.0)

    async def get_all(self, assets: list[str]) -> dict[str, OnchainData]:
        if not assets:
            return {}
        funding_body, oi_body = await asyncio.gather(
            self._call("get_funding_rate", {"assets": assets}),
            self._call("get_open_interest", {"assets": assets}),
        )
        funding: dict[str, dict] = funding_body["result"]
        oi: dict[str, dict] = oi_body["result"]

        result: dict[str, OnchainData] = {}
        for asset in assets:
            f = funding.get(asset, {})
            o = oi.get(asset, {})
            result[asset] = OnchainData(
                funding_rate=f.get("funding_rate"),
                annualized_funding_pct=f.get("annualized_pct"),
                next_funding_time=f.get("next_funding_time"),
                oi_usd=o.get("oi_usd"),
                oi_change_pct_24h=o.get("change_pct_24h"),
                # TODO: liquidation data unavailable — Binance /fapi/v1/allForceOrders
                # was decommissioned. Either source from another provider or remove these fields.
                liquidated_usd_15m=None,
                long_liquidated_usd=None,
                short_liquidated_usd=None,
            )
        return result
