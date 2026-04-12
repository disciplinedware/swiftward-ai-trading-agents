import httpx

from common.exceptions import MCPError

_BASE_URL = "https://fapi.binance.com"


def asset_to_symbol(asset: str) -> str:
    """Map short asset name to Binance Futures symbol (e.g. 'BTC' -> 'BTCUSDT')."""
    return f"{asset}USDT"


class BinanceFuturesClient:
    """Async Binance Futures public REST client (no auth required)."""

    def __init__(self) -> None:
        self._http: httpx.AsyncClient | None = None

    async def connect(self) -> None:
        self._http = httpx.AsyncClient(base_url=_BASE_URL, timeout=10.0)

    async def close(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def get_premium_index(self, symbol: str) -> dict:
        """Fetch current mark price and funding rate for a symbol.

        Response fields used: lastFundingRate, nextFundingTime.
        """
        return await self._get("/fapi/v1/premiumIndex", {"symbol": symbol})

    async def get_open_interest_hist(self, symbol: str, period: str, limit: int) -> list[dict]:
        """Fetch historical open interest snapshots.

        Response fields used: sumOpenInterestValue (USD notional), timestamp.
        """
        return await self._get(
            "/futures/data/openInterestHist",
            {"symbol": symbol, "period": period, "limit": limit},
        )

    async def get_force_orders(self, symbol: str, start_ms: int, end_ms: int) -> list[dict]:
        """Fetch forced liquidation orders within a time window.

        NOTE: /fapi/v1/allForceOrders was decommissioned by Binance on 2021-04-27.
        The only replacement (/fapi/v1/forceOrders) requires authentication and returns
        only the authenticated user's orders — not market-wide liquidations.
        Market-wide data is available via WebSocket stream only (!forceOrder@arr).
        """
        raise MCPError(
            f"Binance public liquidation REST API is decommissioned for {symbol}; "
            "use the !forceOrder@arr WebSocket stream instead"
        )

    async def _get(self, path: str, params: dict) -> list | dict:
        if self._http is None:
            raise MCPError("BinanceFuturesClient not connected")
        try:
            resp = await self._http.get(path, params=params)
        except httpx.HTTPError as e:
            raise MCPError(f"Binance Futures request failed: {e}") from e
        if resp.status_code != 200:
            raise MCPError(f"Binance Futures error {resp.status_code} for {path}: {resp.text}")
        try:
            return resp.json()
        except Exception as e:
            raise MCPError(f"Binance Futures returned invalid JSON for {path}") from e
