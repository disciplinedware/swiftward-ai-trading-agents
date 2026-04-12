import httpx

from common.models.portfolio_snapshot import PortfolioSnapshot
from common.models.trade_intent import TradeIntent

from ._mcp_client import MCPClientMixin


class TradingMCPClient(MCPClientMixin):
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = client or httpx.AsyncClient(timeout=60.0)

    async def get_portfolio(self) -> PortfolioSnapshot:
        body = await self._call("get_portfolio", {})
        return PortfolioSnapshot.model_validate(body["result"])

    async def execute_swap(self, intent: TradeIntent) -> dict:
        body = await self._call(
            "execute_swap", {"intent": intent.model_dump(mode="json")}
        )
        return body["result"]

    async def end_cycle(self, summary: str) -> None:
        """No-op: Python trading MCP does not support end_cycle."""
