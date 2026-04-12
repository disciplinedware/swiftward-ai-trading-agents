from decimal import Decimal

import httpx

from common.exceptions import MCPError

_BASE_URL = "https://api.binance.com"


def asset_to_symbol(asset: str) -> str:
    """Map short asset name to Binance symbol (e.g. 'BTC' -> 'BTCUSDT')."""
    return f"{asset}USDT"


class BinanceClient:
    """Async Binance public REST client (no auth required)."""

    def __init__(self) -> None:
        self._http: httpx.AsyncClient | None = None

    async def connect(self) -> None:
        self._http = httpx.AsyncClient(base_url=_BASE_URL, timeout=10.0)

    async def close(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def get_ticker_price(self, symbol: str) -> Decimal:
        """Fetch current price for a symbol."""
        data = await self._get("/api/v3/ticker/price", {"symbol": symbol})
        try:
            return Decimal(data["price"])
        except (KeyError, Exception) as e:
            raise MCPError(f"Unexpected ticker response for {symbol}: {data}") from e

    # GET /api/v3/klines: https://developers.binance.com/docs/binance-spot-api-docs/testnet/rest-api/market-data-endpoints#klinecandlestick-data
    # Response
    # [
    #     [
    #         1499040000000,         // Kline open time
    #         "0.01634790",          // Open price
    #         "0.80000000",          // High price
    #         "0.01575800",          // Low price
    #         "0.01577100",          // Close price
    #         "148976.11427815",     // Volume
    #         1499644799999,         // Kline Close time
    #         "2434.19055334",       // Quote asset volume
    #         308,                   // Number of trades
    #         "1756.87402397",       // Taker buy base asset volume
    #         "28.46694368",         // Taker buy quote asset volume
    #         "0"                    // Unused field, ignore.
    #     ]
    # ]
    async def get_klines(self, symbol: str, interval: str, limit: int) -> list[list]:
        """Fetch historical klines. Each kline is a list of values per Binance spec."""
        return await self._get(
            "/api/v3/klines", {"symbol": symbol, "interval": interval, "limit": limit}
        )

    async def _get(self, path: str, params: dict) -> list | dict:
        if self._http is None:
            raise MCPError("BinanceClient not connected")
        try:
            resp = await self._http.get(path, params=params)
        except httpx.HTTPError as e:
            raise MCPError(f"Binance request failed: {e}") from e
        if resp.status_code != 200:
            raise MCPError(f"Binance error {resp.status_code} for {path}: {resp.text}")
        try:
            return resp.json()
        except Exception as e:
            raise MCPError(f"Binance returned invalid JSON for {path}") from e
