import httpx

from common.models.signal_bundle import FearGreedData

from ._mcp_client import MCPClientMixin


class FearGreedMCPClient(MCPClientMixin):
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=30.0)

    async def get_index(self) -> FearGreedData:
        body = await self._call("get_index", {})
        r = body["result"]
        return FearGreedData(
            value=r.get("value", 50),
            classification=r.get("classification", "Neutral"),
            timestamp=r.get("timestamp", ""),
        )
