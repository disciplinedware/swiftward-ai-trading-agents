"""Trading MCP server — FastMCP app on port 8005.

Exposes tools:
  execute_swap, get_portfolio, get_positions, get_position, get_daily_pnl

Startup (lifespan):
  1. Wire all deps: DB engine, session factory, PortfolioService, PriceClient,
     ERC8004Registry, PaperEngine or LiveEngine, TradingService
  2. Await register_identity — agentId required before engine init

Note: Alembic migrations run before server start via the compose service command:
  uv run alembic -c src/trading_mcp/infra/alembic.ini upgrade head
"""
import asyncio
from contextlib import asynccontextmanager
from decimal import Decimal

from mcp.server.fastmcp import FastMCP
from starlette.requests import Request
from starlette.responses import JSONResponse

from common.config import get_config
from common.log import get_logger, setup_logging
from trading_mcp.erc8004.ipfs import MockIpfs, PinataIpfs
from trading_mcp.erc8004.registry import ERC8004Config, ERC8004Registry
from trading_mcp.infra.db import make_engine, make_session_factory
from trading_mcp.infra.price_client import PriceClient
from trading_mcp.service.portfolio_service import PortfolioService
from trading_mcp.service.trading_service import TradingService

logger = get_logger(__name__)

# Module-level service instance — set in lifespan, read by tool handlers
_service: TradingService | None = None
_db_engine = None


def _get_service() -> TradingService:
    if _service is None:
        raise RuntimeError("TradingService not initialized — server still starting up")
    return _service




# ---------------------------------------------------------------------------
# Lifespan
# ---------------------------------------------------------------------------

@asynccontextmanager
async def _lifespan(app):
    global _service, _db_engine

    # stateless_http=True reruns lifespan per request — only init once
    if _service is not None:
        yield
        return

    cfg = get_config()
    setup_logging(cfg)
    logger.info("trading_mcp starting", mode=cfg.trading.mode)

    # 1. Build DB layer
    _db_engine = make_engine(cfg.trading.database_url)
    session_factory = make_session_factory(_db_engine)

    # 3. PortfolioService
    portfolio_service = PortfolioService(
        session_factory=session_factory,
        starting_balance_usdc=Decimal(str(cfg.trading.starting_balance_usdc)),
    )

    # 4. PriceClient
    price_client = PriceClient(base_url=cfg.mcp_servers.price_feed_url)

    # 5. IPFS provider
    if cfg.erc8004.ipfs_provider == "pinata":
        ipfs = PinataIpfs(jwt=cfg.erc8004.ipfs_api_key)
    else:
        ipfs = MockIpfs()

    # 6. ERC8004 registry
    wallet_address = _derive_wallet_address(cfg.erc8004.agent_wallet_private_key)
    erc8004_cfg = ERC8004Config(
        chain_id=cfg.chain.chain_id,
        rpc_url=cfg.chain.rpc_url,
        identity_registry_address=cfg.erc8004.identity_registry_address,
        validation_registry_address=cfg.erc8004.validation_registry_address,
        wallet_address=wallet_address,
        wallet_private_key=cfg.erc8004.agent_wallet_private_key,
        tracked_assets=cfg.assets.tracked,
        ipfs_provider=cfg.erc8004.ipfs_provider,
        ipfs_api_key=cfg.erc8004.ipfs_api_key,
    )
    # Shared lock for all on-chain txs from this wallet (engine + registry)
    wallet_tx_lock = asyncio.Lock()

    registry = ERC8004Registry(
        config=erc8004_cfg,
        ipfs=ipfs,
        session_factory=session_factory,
        tx_lock=wallet_tx_lock,
    )

    # 7. Register identity (blocking — agent_id required before engine init)
    agent_id = await registry.register_identity()
    if agent_id is None:
        raise RuntimeError("ERC-8004 identity registration failed — cannot start server")

    # 8. Select engine
    if cfg.trading.mode == "live":
        from trading_mcp.engine.live import LiveEngine

        engine = LiveEngine(
            risk_router_address=cfg.chain.risk_router_address,
            chain_id=cfg.chain.chain_id,
            agent_id=agent_id,
            wallet_address=wallet_address,
            wallet_private_key=cfg.erc8004.agent_wallet_private_key,
            rpc_url=cfg.chain.rpc_url,
            tx_lock=wallet_tx_lock,
        )
    else:
        from trading_mcp.engine.paper import PaperEngine

        engine = PaperEngine()

    # 9. TradingService
    _service = TradingService(
        engine=engine,
        portfolio_service=portfolio_service,
        price_client=price_client,
        registry=registry,
        max_concurrent_positions=cfg.trading.max_concurrent_positions,
    )
    logger.info("trading_mcp ready", mode=cfg.trading.mode, port=8005, agent_id=agent_id)

    yield

    # Cleanup
    if _db_engine is not None:
        await _db_engine.dispose()
    logger.debug("trading_mcp lifespan cleanup")


def _derive_wallet_address(private_key: str) -> str:
    """Derive an Ethereum wallet address from a private key hex string."""
    try:
        from eth_account import Account  # part of web3

        account = Account.from_key(private_key)
        return account.address
    except Exception:
        # In mock/test mode the private key may be a placeholder
        return "0x0000000000000000000000000000000000000000"


# ---------------------------------------------------------------------------
# FastMCP app (declared before health route decorator)
# ---------------------------------------------------------------------------


mcp = FastMCP(
    "trading_mcp",
    stateless_http=True,
    lifespan=_lifespan,
    host="0.0.0.0",
    port=8005,
)


@mcp.custom_route("/health", methods=["GET"])
async def _health(request: Request) -> JSONResponse:
    return JSONResponse({"status": "ok"})


@mcp.tool()
async def execute_swap(intent: dict) -> dict:
    """Execute a swap (LONG, FLAT, or FLAT_ALL).

    Accepts a TradeIntent dict and returns an ExecutionResult dict.
    FLAT_ALL closes all open positions and returns {"status", "trades": [...]}.
    """
    from common.models.trade_intent import TradeIntent

    validated = TradeIntent.model_validate(intent)
    if validated.action == "FLAT_ALL":
        return await _get_service().execute_flat_all(validated)
    result = await _get_service().execute_swap(validated)
    return result.to_dict()


@mcp.tool()
async def get_portfolio() -> dict:
    """Return full portfolio state with unrealized PnL for open positions."""
    summary = await _get_service().get_portfolio()
    return summary.to_dict()


@mcp.tool()
async def get_positions() -> list[dict]:
    """Return all open positions with live unrealized PnL."""
    views = await _get_service().get_positions()
    return [v.to_dict() for v in views]


@mcp.tool()
async def get_position(asset: str) -> dict | None:
    """Return the open position for a specific asset, or None.

    Args:
        asset: Asset symbol (e.g. "ETH").
    """
    view = await _get_service().get_position(asset)
    if view is None:
        return None
    return view.to_dict()


@mcp.tool()
async def get_daily_pnl() -> str:
    """Return today's realized PnL (UTC) as a Decimal string."""
    pnl = await _get_service().get_daily_pnl()
    return str(pnl)


# ---------------------------------------------------------------------------
# Entrypoint
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    mcp.run(transport="streamable-http")
