from contextlib import asynccontextmanager
from contextvars import ContextVar

from mcp.server.fastmcp import FastMCP
from starlette.requests import Request
from starlette.responses import JSONResponse

from common.cache import RedisCache
from common.config import get_config
from common.exceptions import MCPError
from common.log import setup_logging
from price_feed_mcp.infra.binance import BinanceClient
from price_feed_mcp.service.price_feed import PriceFeedService

_service_var: ContextVar[PriceFeedService | None] = ContextVar("_service", default=None)


@asynccontextmanager
async def _lifespan(_: FastMCP):
    cfg = get_config()
    setup_logging(cfg)
    cache = RedisCache(cfg.cache.redis_url)
    await cache.connect()
    binance = BinanceClient()
    await binance.connect()
    token = _service_var.set(PriceFeedService(binance, cache))

    yield

    _service_var.reset(token)
    await cache.close()
    await binance.close()


mcp = FastMCP("price_feed", stateless_http=True, lifespan=_lifespan, host="0.0.0.0", port=8001)


def _get_service() -> PriceFeedService:
    svc = _service_var.get()
    if svc is None:
        raise MCPError("Server not initialized")
    return svc


@mcp.custom_route("/health", methods=["GET"])
async def health(_: Request) -> JSONResponse:
    return JSONResponse({"status": "ok"})


@mcp.tool()
async def get_prices_latest(assets: list[str]) -> dict[str, str]:
    """Return current price for each requested asset."""
    return await _get_service().get_prices_latest(assets)


@mcp.tool()
async def get_prices_change(assets: list[str]) -> dict[str, dict[str, str]]:
    """Return % price change across 1m/5m/1h/4h/24h windows for each asset."""
    return await _get_service().get_prices_change(assets)


@mcp.tool()
async def get_indicators(assets: list[str]) -> dict[str, dict]:
    """Return technical indicators for each asset."""
    return await _get_service().get_indicators(assets)


if __name__ == "__main__":
    mcp.run(transport="streamable-http")
