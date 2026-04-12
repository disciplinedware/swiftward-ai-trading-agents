from datetime import datetime, timezone

import httpx

from common.exceptions import MCPError
from common.log import get_logger
from news_mcp.infra.base import NewsPost

logger = get_logger(__name__)

_BASE_URL = "https://data-api.coindesk.com"


class CoinDeskClient:
    """Async CoinDesk Data API client."""

    def __init__(self, api_key: str) -> None:
        self._api_key = api_key
        self._http: httpx.AsyncClient | None = None

    async def connect(self) -> None:
        self._http = httpx.AsyncClient(base_url=_BASE_URL, timeout=10.0)

    async def close(self) -> None:
        if self._http is not None:
            await self._http.aclose()
            self._http = None

    async def get_posts(self, currencies: list[str], limit: int = 50) -> list[NewsPost]:  # noqa: ARG002
        """Fetch recent news articles. currencies param is unused — CoinDesk provides a general feed."""  # noqa: E501
        params = {
            "api_key": self._api_key,
            "lang": "EN",
            "limit": limit,
        }
        data = await self._get("/news/v1/article/list", params)
        articles = data.get("Data", [])
        logger.debug("CoinDesk response", total_articles=len(articles))
        parsed = [_parse_article(a) for a in articles]
        logger.debug("CoinDesk articles parsed", total=len(parsed))
        return parsed

    async def _get(self, path: str, params: dict) -> dict:
        if self._http is None:
            raise MCPError("CoinDeskClient not connected")
        try:
            resp = await self._http.get(path, params=params)
        except httpx.HTTPError as e:
            raise MCPError(f"CoinDesk request failed: {e}") from e
        if resp.status_code != 200:
            logger.error(
                "CoinDesk non-200 response",
                status=resp.status_code,
                body=resp.text[:300],
            )
            raise MCPError(f"CoinDesk error {resp.status_code}: {resp.text[:200]}")
        try:
            return resp.json()
        except Exception as e:
            raise MCPError("CoinDesk returned invalid JSON") from e


def _parse_article(raw: dict) -> NewsPost:
    published_on = raw.get("PUBLISHED_ON")
    if published_on:
        published_at = datetime.fromtimestamp(published_on, tz=timezone.utc).isoformat()
    else:
        published_at = ""

    source_data = raw.get("SOURCE_DATA") or {}
    source = source_data.get("NAME") or source_data.get("name") or ""

    return {
        "title": raw.get("TITLE") or "",
        "description": raw.get("BODY") or "",
        "url": raw.get("URL") or "",
        "published_at": published_at,
        "source": source,
    }
