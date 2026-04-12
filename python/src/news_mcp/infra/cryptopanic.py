import httpx

from common.exceptions import MCPError
from common.log import get_logger
from news_mcp.infra.base import NewsPost

logger = get_logger(__name__)

_BASE_URL = "https://cryptopanic.com/api/developer/v2"


class CryptoPanicClient:
    """Async CryptoPanic REST client. Returns raw post dicts."""

    def __init__(self, auth_token: str) -> None:
        self._token = auth_token
        self._http: httpx.AsyncClient | None = None

    async def connect(self) -> None:
        self._http = httpx.AsyncClient(base_url=_BASE_URL, timeout=10.0)

    async def close(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def get_posts(self, currencies: list[str], limit: int = 50) -> list[NewsPost]:
        """Fetch recent news posts for the given currency codes."""
        params = {
            "auth_token": self._token,
            "currencies": ",".join(currencies),
            "kind": "news",
            "public": "true",
        }
        data = await self._get("/posts/", params)
        results = data.get("results", [])
        logger.debug("CryptoPanic response", total_results=len(results), next=data.get("next"))
        parsed = [_parse_post(r) for r in results[:limit]]
        logger.debug("CryptoPanic posts parsed", total=len(parsed))
        return parsed

    async def _get(self, path: str, params: dict) -> dict:
        if self._http is None:
            raise MCPError("CryptoPanicClient not connected")
        try:
            resp = await self._http.get(path, params=params)
        except httpx.HTTPError as e:
            raise MCPError(f"CryptoPanic request failed: {e}") from e
        if resp.status_code != 200:
            logger.error(
                "CryptoPanic non-200 response",
                status=resp.status_code,
                body=resp.text[:300],
            )
            raise MCPError(f"CryptoPanic error {resp.status_code}: {resp.text[:200]}")
        try:
            return resp.json()
        except Exception as e:
            raise MCPError("CryptoPanic returned invalid JSON") from e


def _parse_post(raw: dict) -> NewsPost:
    return {
        "title": raw.get("title", ""),
        "description": raw.get("description") or "",
        "url": raw.get("url", ""),
        "published_at": raw.get("published_at", ""),
        "source": (raw.get("source") or {}).get("title", ""),
    }
