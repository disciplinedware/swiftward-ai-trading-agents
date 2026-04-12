from contextlib import asynccontextmanager
from contextvars import ContextVar

from mcp.server.fastmcp import FastMCP
from starlette.requests import Request
from starlette.responses import JSONResponse

from common.cache import RedisCache
from common.config import get_config
from common.exceptions import MCPError
from common.log import setup_logging
from news_mcp.infra.coindesk import CoinDeskClient
from news_mcp.service.llm import NewsLLMScorer
from news_mcp.service.news import NewsService

_service_var: ContextVar[NewsService | None] = ContextVar("_service", default=None)
_tracked_assets_var: ContextVar[list[str]] = ContextVar("_tracked_assets", default=[])


@asynccontextmanager
async def _lifespan(_: FastMCP):
    cfg = get_config()
    setup_logging(cfg)
    tok_assets = _tracked_assets_var.set(cfg.assets.tracked)

    cache = RedisCache(cfg.cache.redis_url)
    await cache.connect()

    client = CoinDeskClient(cfg.external_apis.coindesk_api_key)
    await client.connect()

    scorer = NewsLLMScorer(cfg.news_llm)
    tok_svc = _service_var.set(NewsService(client, scorer, cache, cfg.assets.tracked))

    yield

    _service_var.reset(tok_svc)
    _tracked_assets_var.reset(tok_assets)
    await client.close()
    await cache.close()


mcp = FastMCP("news", stateless_http=True, lifespan=_lifespan, host="0.0.0.0", port=8002)


def _get_service() -> NewsService:
    svc = _service_var.get()
    if svc is None:
        raise MCPError("Server not initialized")
    return svc


@mcp.custom_route("/health", methods=["GET"])
async def health(_: Request) -> JSONResponse:
    return JSONResponse({"status": "ok"})


@mcp.tool()
async def get_headlines() -> list[dict]:
    """Return recent headlines as a flat list. Each: {title, url, published_at, source}."""
    return await _get_service().get_headlines()  # type: ignore[return-value]


@mcp.tool()
async def get_sentiment(assets: list[str]) -> dict[str, float]:
    """Return per-asset sentiment score -1.0 (bearish) to +1.0 (bullish). Cached 5 min."""
    return await _get_service().get_sentiment(assets)


@mcp.tool()
async def get_macro_flag() -> dict:
    """Return global macro event flag {triggered: bool, reason: str|null}.
    True for Fed policy changes, ETF events, exchange collapse/hack, regulatory crackdown."""
    return await _get_service().get_macro_flag(_tracked_assets_var.get())


if __name__ == "__main__":
    mcp.run(transport="streamable-http")
