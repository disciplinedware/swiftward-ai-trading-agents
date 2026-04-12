import httpx

from common.models.signal_bundle import NewsData

from ._mcp_client import MCPClientMixin


class NewsMCPClient(MCPClientMixin):
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=60.0)

    async def get_signals(self, assets: list[str]) -> dict[str, NewsData]:
        if not assets:
            return {}
        sentiment_body = await self._call("get_sentiment", {"assets": assets})
        macro_body = await self._call("get_macro_flag", {})
        sentiments: dict[str, float] = sentiment_body["result"]
        macro: dict = macro_body["result"]
        macro_flag: bool = macro.get("triggered", False)

        return {
            asset: NewsData(
                sentiment=sentiments.get(asset, 0.0),
                macro_flag=macro_flag,
            )
            for asset in assets
        }
