"""Price client — fetches current asset prices from price_feed_mcp via MCP."""
from decimal import Decimal

import httpx

from common.exceptions import MCPError
from common.log import get_logger
from common.mcp_client import MCPClientMixin

logger = get_logger(__name__)


class PriceClient(MCPClientMixin):
    """Async HTTP client for price_feed_mcp.

    Calls the `get_prices_latest` MCP tool over FastMCP streamable-http.
    """

    def __init__(self, base_url: str, timeout: float = 10.0) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = httpx.AsyncClient(timeout=timeout)

    async def get_prices_latest(self, assets: list[str]) -> dict[str, Decimal]:
        """Fetch current prices for multiple assets.

        Returns a dict mapping asset symbol to Decimal price.
        Raises MCPError if the upstream returns an error.
        """
        body = await self._call("get_prices_latest", {"assets": assets})
        prices_raw: dict = body["result"]

        prices: dict[str, Decimal] = {}
        for asset, value in prices_raw.items():
            try:
                prices[asset] = Decimal(str(value))
            except Exception as exc:
                raise MCPError(
                    f"price_feed_mcp returned non-numeric price for {asset!r}: {value!r}"
                ) from exc

        logger.debug("prices fetched", assets=assets, count=len(prices))
        return prices

    async def get_price(self, asset: str) -> Decimal:
        """Fetch current price for a single asset.

        Raises MCPError if the asset is not found in the response or on error.
        """
        prices = await self.get_prices_latest([asset])
        if asset not in prices:
            raise MCPError(f"price_feed_mcp did not return price for {asset!r}")
        return prices[asset]
