import httpx

from common.exceptions import MCPError

_BASE_URL = "https://api.alternative.me"


class AlternativeMeClient:
    """Async HTTP client for the Alternative.me Fear & Greed Index API (no auth required)."""

    def __init__(self) -> None:
        self._http: httpx.AsyncClient | None = None

    async def connect(self) -> None:
        self._http = httpx.AsyncClient(base_url=_BASE_URL, timeout=10.0)

    async def close(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def get_index(self) -> dict:
        """Fetch the current Fear & Greed index (most recent data point)."""
        data = await self._get("/fng/", {"limit": 1})
        return self._first(data)

    async def get_historical(self, limit: int) -> list[dict]:
        """Fetch the last `limit` daily Fear & Greed values."""
        data = await self._get("/fng/", {"limit": limit})
        return data["data"]

    # ------------------------------------------------------------------

    async def _get(self, path: str, params: dict) -> dict:
        if self._http is None:
            raise MCPError("AlternativeMeClient not connected")
        try:
            resp = await self._http.get(path, params=params)
        except httpx.HTTPError as e:
            raise MCPError(f"Alternative.me request failed: {e}") from e
        if resp.status_code != 200:
            raise MCPError(f"Alternative.me error {resp.status_code}: {resp.text}")
        try:
            return resp.json()
        except Exception as e:
            raise MCPError("Alternative.me returned invalid JSON") from e

    @staticmethod
    def _first(data: dict) -> dict:
        try:
            return data["data"][0]
        except (KeyError, IndexError) as e:
            raise MCPError(f"Unexpected Alternative.me response shape: {data}") from e
