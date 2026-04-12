from contextlib import asynccontextmanager

from mcp.server.fastmcp import FastMCP
from starlette.requests import Request
from starlette.responses import JSONResponse

from common.cache import RedisCache
from common.config import get_config
from common.exceptions import MCPError
from common.log import setup_logging
from fear_greed_mcp.infra.alternative_me import AlternativeMeClient
from fear_greed_mcp.service.fear_greed import FearGreedService

_service: FearGreedService | None = None


@asynccontextmanager
async def _lifespan(_: FastMCP):
    global _service

    cfg = get_config()
    setup_logging(cfg)
    cache = RedisCache(cfg.cache.redis_url)
    await cache.connect()
    client = AlternativeMeClient()
    await client.connect()
    _service = FearGreedService(client, cache)

    yield

    await client.close()
    await cache.close()


mcp = FastMCP("fear_greed", stateless_http=True, lifespan=_lifespan, host="0.0.0.0", port=8004)


def _get_service() -> FearGreedService:
    if _service is None:
        raise MCPError("Server not initialized")
    return _service


@mcp.custom_route("/health", methods=["GET"])
async def health(_: Request) -> JSONResponse:
    return JSONResponse({"status": "ok"})


@mcp.tool()
async def get_index() -> dict:
    """Return the current Crypto Fear & Greed index value, classification, and timestamp."""
    return await _get_service().get_index()


@mcp.tool()
async def get_historical(limit: int) -> list[dict]:
    """Return the last `limit` daily Fear & Greed values (always fetched fresh)."""
    return await _get_service().get_historical(limit)


if __name__ == "__main__":
    mcp.run(transport="streamable-http")
