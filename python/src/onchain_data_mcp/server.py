from contextlib import asynccontextmanager
from contextvars import ContextVar

from mcp.server.fastmcp import FastMCP
from starlette.requests import Request
from starlette.responses import JSONResponse

from common.cache import RedisCache
from common.config import get_config
from common.exceptions import MCPError
from common.log import setup_logging
from onchain_data_mcp.infra.binance_futures import BinanceFuturesClient
from onchain_data_mcp.service.onchain_data import OnchainDataService

# ContextVar gives each concurrent request its own service instance,
# avoiding the race condition where concurrent requests share a global
# that gets closed mid-request in stateless_http mode.
_service_var: ContextVar[OnchainDataService | None] = ContextVar("_service", default=None)


@asynccontextmanager
async def _lifespan(_: FastMCP):
    cfg = get_config()
    setup_logging(cfg)
    cache = RedisCache(cfg.cache.redis_url)
    await cache.connect()
    client = BinanceFuturesClient()
    await client.connect()
    token = _service_var.set(OnchainDataService(client, cache))

    yield

    _service_var.reset(token)
    await client.close()
    await cache.close()


mcp = FastMCP("onchain_data", stateless_http=True, lifespan=_lifespan, host="0.0.0.0", port=8003)


def _get_service() -> OnchainDataService:
    svc = _service_var.get()
    if svc is None:
        raise MCPError("Server not initialized")
    return svc


@mcp.custom_route("/health", methods=["GET"])
async def health(_: Request) -> JSONResponse:
    return JSONResponse({"status": "ok"})


@mcp.tool()
async def get_funding_rate(assets: list[str]) -> dict[str, dict]:
    """Return current funding rate and annualized % for each requested asset."""
    return await _get_service().get_funding_rate(assets)


@mcp.tool()
async def get_open_interest(assets: list[str]) -> dict[str, dict]:
    """Return open interest (USD) and 24h % change for each requested asset."""
    return await _get_service().get_open_interest(assets)


@mcp.tool()
async def get_liquidations(assets: list[str]) -> dict[str, dict]:
    """Return USD liquidated in the last 15-min window per asset (long and short separately)."""
    return await _get_service().get_liquidations(assets)


@mcp.tool()
async def get_netflow() -> dict[str, dict]:
    """Return BTC/ETH exchange netflow direction (inflow=sell pressure, outflow=accumulation)."""
    return await _get_service().get_netflow()


if __name__ == "__main__":
    mcp.run(transport="streamable-http")
