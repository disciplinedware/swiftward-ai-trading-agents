"""TradingService — orchestration layer between FastMCP tools and the execution engine."""
import asyncio
from decimal import Decimal
from typing import Union

from common.log import get_logger
from common.models.trade_intent import TradeIntent
from trading_mcp.engine.interface import Engine, ExecutionResult
from trading_mcp.erc8004.registry import ERC8004Registry
from trading_mcp.infra.price_client import PriceClient
from trading_mcp.service.dto import PortfolioSummary, PositionView
from trading_mcp.service.portfolio_service import PortfolioService

logger = get_logger(__name__)


class TradingService:
    """Orchestration layer: fetches prices, calls engine, records to DB, fires ERC-8004 hooks."""

    def __init__(
        self,
        engine: Engine,
        portfolio_service: PortfolioService,
        price_client: PriceClient,
        registry: ERC8004Registry,
        max_concurrent_positions: int,
    ) -> None:
        self._engine = engine
        self._portfolio = portfolio_service
        self._prices = price_client
        self._registry = registry
        self._max_positions = max_concurrent_positions

    async def execute_swap(self, intent: TradeIntent) -> ExecutionResult:
        """Fetch price, call engine, record to DB, fire ERC-8004 hooks, return result."""
        if intent.asset is None:
            raise ValueError("execute_swap requires intent.asset to be set")

        if intent.action == "LONG":
            return await self._execute_long(intent)
        if intent.action == "FLAT":
            return await self._execute_flat(intent)

        price = await self._prices.get_price(intent.asset)
        result = await self._engine.execute_swap(intent, price, Decimal("0"))
        logger.info(
            "execute_swap completed",
            asset=intent.asset,
            action=intent.action,
            status=result.status,
            price=str(price),
        )
        return result

    async def execute_flat_all(self, intent: TradeIntent) -> dict:
        """Close all open positions. Returns aggregated result dict.

        No-op (empty trades list) if no positions are open.
        """
        position_sizes = await self._portfolio.get_open_positions_with_sizes()
        if not position_sizes:
            logger.info("execute_flat_all: no open positions")
            return {"status": "executed", "trades": []}

        trades = []
        for asset, (entry_size_usd, entry_price) in position_sizes.items():
            price = await self._prices.get_price(asset)
            size_usd = (entry_size_usd * price / entry_price).quantize(Decimal("0.00000001"))
            flat_intent = TradeIntent(
                asset=asset,
                action="FLAT",
                size_pct=intent.size_pct,
                stop_loss=Decimal("0"),
                take_profit=Decimal("0"),
                strategy=intent.strategy,
                reasoning=intent.reasoning,
                trigger_reason=intent.trigger_reason,
                confidence=intent.confidence,
            )
            result = await self._engine.execute_swap(flat_intent, price, size_usd)
            if result.status == "executed":
                await self._portfolio.record_close(asset, result)
            logger.info(
                "execute_flat_all: closed position",
                asset=asset,
                status=result.status,
                price=str(price),
            )
            trades.append({
                "asset": asset,
                "status": result.status,
                "tx_hash": result.tx_hash,
                "executed_price": str(result.executed_price),
                "reason": result.reason,
            })

        return {"status": "executed", "trades": trades}

    async def get_portfolio(self) -> PortfolioSummary:
        """Fetch prices for open position assets, delegate to portfolio_service."""
        assets = await self._portfolio.get_open_asset_symbols()
        prices = await self._prices.get_prices_latest(assets) if assets else {}
        return await self._portfolio.get_portfolio(prices)

    async def get_positions(self) -> list[PositionView]:
        """Fetch prices for open position assets, return open positions with live PnL."""
        assets = await self._portfolio.get_open_asset_symbols()
        prices = await self._prices.get_prices_latest(assets) if assets else {}
        return await self._portfolio.get_positions(prices)

    async def get_position(self, asset: str) -> Union[PositionView, None]:
        """Return the open position for an asset with live PnL, or None."""
        if not await self._portfolio.has_open_position(asset):
            return None
        price = await self._prices.get_price(asset)
        return await self._portfolio.get_position(asset, price)

    async def get_daily_pnl(self) -> Decimal:
        """Return realized PnL today (UTC) — no price fetch required."""
        return await self._portfolio.get_daily_pnl()

    # ------------------------------------------------------------------
    # Private helpers
    # ------------------------------------------------------------------

    async def _execute_long(self, intent: TradeIntent) -> ExecutionResult:
        assert intent.asset is not None  # guarded in execute_swap
        # Fast capacity pre-check — avoids hitting the engine when obviously full
        if not await self._portfolio.can_open_position(self._max_positions):
            logger.info(
                "execute_swap rejected: at position limit",
                asset=intent.asset,
                max=self._max_positions,
            )
            price = await self._prices.get_price(intent.asset)
            return ExecutionResult(
                status="rejected",
                tx_hash="",
                executed_price=price,
                slippage_pct=Decimal("0"),
                size_usd=Decimal("0"),
                intent_hash=b"\x00" * 32,
                reason="max_concurrent_positions reached",
            )

        stablecoin, _, _, _ = await self._portfolio.get_balance_state()
        amount_usd = (stablecoin * Decimal(str(intent.size_pct))).quantize(
            Decimal("0.00000001")
        )
        price = await self._prices.get_price(intent.asset)
        result = await self._engine.execute_swap(intent, price, amount_usd)

        if result.status == "executed":
            position_id = await self._portfolio.record_open(intent, result)
            asyncio.create_task(
                self._registry.submit_validation(
                    position_id, result.intent_hash, intent.confidence,
                )
            )

        logger.info(
            "execute_swap completed",
            asset=intent.asset,
            action="LONG",
            status=result.status,
            price=str(price),
        )
        return result

    async def _execute_flat(self, intent: TradeIntent) -> ExecutionResult:
        assert intent.asset is not None  # guarded in execute_swap
        pos_info = await self._portfolio.get_open_position_size(intent.asset)
        if pos_info is None:
            price = await self._prices.get_price(intent.asset)
            logger.info(
                "execute_swap rejected: no open position for FLAT",
                asset=intent.asset,
            )
            return ExecutionResult(
                status="rejected",
                tx_hash="",
                executed_price=price,
                slippage_pct=Decimal("0"),
                size_usd=Decimal("0"),
                intent_hash=b"\x00" * 32,
                reason=f"No open position for {intent.asset!r}",
            )

        entry_size_usd, entry_price = pos_info
        price = await self._prices.get_price(intent.asset)
        # Current market value: same token quantity at current price
        size_usd = (entry_size_usd * price / entry_price).quantize(Decimal("0.00000001"))
        result = await self._engine.execute_swap(intent, price, size_usd)

        if result.status == "executed":
            await self._portfolio.record_close(intent.asset, result)

        logger.info(
            "execute_swap completed",
            asset=intent.asset,
            action="FLAT",
            status=result.status,
            price=str(price),
        )
        return result
