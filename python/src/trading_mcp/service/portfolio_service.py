import asyncio
from datetime import datetime, timezone
from decimal import Decimal
from typing import Optional

from sqlalchemy import func, select, text
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from common.models.trade_intent import TradeIntent
from trading_mcp.domain.entity.portfolio_snapshot import PortfolioSnapshot
from trading_mcp.domain.entity.position import Position
from trading_mcp.domain.entity.trade import Trade
from trading_mcp.engine.interface import ExecutionResult
from trading_mcp.service.dto import PortfolioSummary, PositionView


def _unrealized_pnl(position: Position, current_price: Decimal) -> tuple[Decimal, Decimal]:
    """Return (unrealized_pnl_usd, unrealized_pnl_pct) for an open LONG position."""
    if position.entry_price == 0:
        return Decimal("0"), Decimal("0")
    price_return = (current_price - position.entry_price) / position.entry_price
    pnl_usd = position.size_usd * price_return
    return pnl_usd.quantize(Decimal("0.00000001")), price_return.quantize(Decimal("0.00000001"))


def _to_view(position: Position, current_price: Decimal) -> PositionView:
    pnl_usd, pnl_pct = _unrealized_pnl(position, current_price)
    return PositionView(
        id=position.id,
        asset=position.asset,
        status=position.status,
        action=position.action,
        entry_price=position.entry_price,
        size_usd=position.size_usd,
        size_pct=position.size_pct,
        stop_loss=position.stop_loss,
        take_profit=position.take_profit,
        strategy=position.strategy,
        trigger_reason=position.trigger_reason,
        reasoning=position.reasoning,
        opened_at=position.opened_at,
        tx_hash_open=position.tx_hash_open,
        unrealized_pnl_usd=pnl_usd,
        unrealized_pnl_pct=pnl_pct,
        current_price=current_price,
        validation_uri=position.validation_uri,
        closed_at=position.closed_at,
        exit_reason=position.exit_reason,
        exit_price=position.exit_price,
        realized_pnl_usd=position.realized_pnl_usd,
        realized_pnl_pct=position.realized_pnl_pct,
        tx_hash_close=position.tx_hash_close,
    )


class PortfolioService:
    """Async service for reading and writing portfolio state.

    Injected with an async_sessionmaker. All write paths are protected by
    an asyncio.Lock to prevent concurrent mutations.
    """

    def __init__(
        self,
        session_factory: async_sessionmaker[AsyncSession],
        starting_balance_usdc: Decimal,
    ) -> None:
        self._sf = session_factory
        self._starting_balance = starting_balance_usdc
        self._lock = asyncio.Lock()

    # ------------------------------------------------------------------
    # Read methods
    # ------------------------------------------------------------------

    async def get_portfolio(self, current_prices: dict[str, Decimal]) -> PortfolioSummary:
        """Return full portfolio state with unrealized PnL for open positions."""
        async with self._sf() as session:
            open_positions = await self._fetch_open_positions(session)
            latest_snap = await self._latest_snapshot(session)
            daily_pnl = await self._sum_daily_pnl(session)

        views = []
        for p in open_positions:
            price = current_prices.get(p.asset)
            if price is None:
                raise KeyError(f"Missing current price for open position asset: {p.asset!r}")
            views.append(_to_view(p, price))

        if latest_snap is not None:
            stablecoin = latest_snap.stablecoin_balance
            drawdown = latest_snap.current_drawdown_pct
            peak = latest_snap.peak_total_usd
        else:
            stablecoin = self._starting_balance
            drawdown = Decimal("0")
            peak = self._starting_balance

        # Mark-to-market: stablecoin + current value of each open position
        total_usd = stablecoin + sum(
            (v.size_usd + v.unrealized_pnl_usd) for v in views
        )

        return PortfolioSummary(
            total_usd=total_usd,
            stablecoin_balance=stablecoin,
            open_position_count=len(views),
            realized_pnl_today=daily_pnl,
            current_drawdown_pct=drawdown,
            peak_total_usd=peak,
            open_positions=views,
        )

    async def get_positions(self, current_prices: dict[str, Decimal]) -> list[PositionView]:
        """Return all open positions with unrealized PnL."""
        async with self._sf() as session:
            positions = await self._fetch_open_positions(session)
        views = []
        for p in positions:
            price = current_prices.get(p.asset)
            if price is None:
                raise KeyError(f"Missing current price for open position asset: {p.asset!r}")
            views.append(_to_view(p, price))
        return views

    async def get_position(
        self, asset: str, current_price: Decimal
    ) -> Optional[PositionView]:
        """Return the open position for a specific asset, or None."""
        async with self._sf() as session:
            result = await session.execute(
                select(Position)
                .where(Position.asset == asset, Position.status == "open")
                .limit(1)
            )
            pos = result.scalar_one_or_none()
        if pos is None:
            return None
        return _to_view(pos, current_price)

    async def get_balance_state(self) -> tuple[Decimal, Decimal, Decimal, Decimal]:
        """Return (stablecoin_balance, realized_pnl_today, peak_total_usd, total_usd).

        Reads from the latest portfolio_snapshot without needing current prices.
        Falls back to starting_balance if no snapshots exist.
        """
        async with self._sf() as session:
            snap = await self._latest_snapshot(session)
            daily_pnl = await self._sum_daily_pnl(session)
        if snap is None:
            return (
                self._starting_balance,
                Decimal("0"),
                self._starting_balance,
                self._starting_balance,
            )
        return snap.stablecoin_balance, daily_pnl, snap.peak_total_usd, snap.total_usd

    async def can_open_position(self, max_positions: int) -> bool:
        """Return False if already at or above the position limit (non-locking fast check).

        Used by TradingService to reject intents before submitting to the engine.
        record_open performs no capacity check — once a trade is executed it is always recorded.
        """
        async with self._sf() as session:
            result = await session.execute(
                select(func.count()).select_from(Position).where(Position.status == "open")
            )
            return result.scalar_one() < max_positions

    async def count_open_positions(self) -> int:
        """Return the number of currently open positions (no price needed)."""
        async with self._sf() as session:
            result = await session.execute(
                select(func.count()).select_from(Position).where(Position.status == "open")
            )
            return result.scalar_one()

    async def has_open_position(self, asset: str) -> bool:
        """Return True if there is an open position for the given asset."""
        async with self._sf() as session:
            result = await session.execute(
                select(func.count())
                .select_from(Position)
                .where(Position.asset == asset, Position.status == "open")
            )
            return result.scalar_one() > 0

    async def get_open_asset_symbols(self) -> list[str]:
        """Return asset symbols for all currently open positions."""
        async with self._sf() as session:
            result = await session.execute(
                select(Position.asset).where(Position.status == "open")
            )
            return list(result.scalars().all())

    async def get_open_position_size(
        self, asset: str,
    ) -> Optional[tuple[Decimal, Decimal]]:
        """Return (size_usd, entry_price) of the open position for asset, or None."""
        async with self._sf() as session:
            result = await session.execute(
                select(Position.size_usd, Position.entry_price)
                .where(Position.asset == asset, Position.status == "open")
                .limit(1)
            )
            row = result.one_or_none()
            return (row[0], row[1]) if row is not None else None

    async def get_open_positions_with_sizes(self) -> dict[str, tuple[Decimal, Decimal]]:
        """Return {asset: (size_usd, entry_price)} for all open positions."""
        async with self._sf() as session:
            result = await session.execute(
                select(Position.asset, Position.size_usd, Position.entry_price)
                .where(Position.status == "open")
            )
            return {row.asset: (row.size_usd, row.entry_price) for row in result.all()}

    async def get_daily_pnl(self) -> Decimal:
        """Return sum of realized_pnl_usd for positions closed today (UTC)."""
        async with self._sf() as session:
            return await self._sum_daily_pnl(session)

    async def get_closed_position_by_asset(self, asset: str) -> Optional[PositionView]:
        """Return the most recently closed position for a specific asset, or None."""
        async with self._sf() as session:
            result = await session.execute(
                select(Position)
                .where(Position.asset == asset, Position.status == "closed")
                .order_by(Position.closed_at.desc())
                .limit(1)
            )
            pos = result.scalar_one_or_none()
        if pos is None:
            return None
        current_price = pos.exit_price if pos.exit_price is not None else pos.entry_price
        return _to_view(pos, current_price)

    # ------------------------------------------------------------------
    # Write methods — record_open / record_close (all engines)
    # ------------------------------------------------------------------

    async def record_open(
        self,
        intent: TradeIntent,
        result: ExecutionResult,
    ) -> int:
        """Atomically persist a new open position after engine execution.

        Returns the position ID.

        Always writes — no capacity check. The trade has already been submitted
        (paper, on-chain, or CEX) so the record must be kept regardless of the
        current position count.

        Under the write lock:
          1. Read current balance state for snapshot computation.
          2. Build and insert Position, Trade, and PortfolioSnapshot atomically.
        """
        async with self._lock:
            async with self._sf() as session:
                async with session.begin():
                    await self._acquire_write_lock(session)
                    snap_row = await self._latest_snapshot(session)
                    daily_pnl = await self._sum_daily_pnl(session)

                    if snap_row is not None:
                        stablecoin = snap_row.stablecoin_balance
                        peak = snap_row.peak_total_usd
                        prev_total = snap_row.total_usd
                    else:
                        stablecoin = self._starting_balance
                        peak = self._starting_balance
                        prev_total = self._starting_balance

                    size_usd = result.size_usd
                    size_pct = Decimal(str(intent.size_pct)).quantize(Decimal("0.00000001"))
                    now = datetime.now(tz=timezone.utc)
                    current_count = await self._count_open_in_session(session)
                    open_count_new = current_count + 1

                    pos = Position(
                        asset=intent.asset,
                        status="open",
                        action="LONG",
                        entry_price=result.executed_price,
                        size_usd=size_usd,
                        size_pct=size_pct,
                        stop_loss=Decimal(str(intent.stop_loss)).quantize(
                            Decimal("0.00000001")
                        ) if intent.stop_loss is not None else Decimal("0"),
                        take_profit=Decimal(str(intent.take_profit)).quantize(
                            Decimal("0.00000001")
                        ) if intent.take_profit is not None else Decimal("0"),
                        strategy=intent.strategy,
                        trigger_reason=intent.trigger_reason,
                        reasoning=intent.reasoning,
                        opened_at=now,
                        tx_hash_open=result.tx_hash,
                    )
                    session.add(pos)
                    await session.flush()

                    trade = Trade(
                        position_id=pos.id,
                        direction="open",
                        asset=intent.asset,
                        price=result.executed_price,
                        size_usd=size_usd,
                        slippage_pct=result.slippage_pct,
                        tx_hash=result.tx_hash,
                        executed_at=now,
                    )
                    session.add(trade)

                    new_stablecoin = (stablecoin - size_usd).quantize(Decimal("0.00000001"))
                    # Opening a position converts stablecoin → position: total stays same
                    new_total = prev_total.quantize(Decimal("0.00000001"))
                    new_peak = max(peak, new_total)
                    drawdown = (
                        ((new_peak - new_total) / new_peak).quantize(Decimal("0.00000001"))
                        if new_peak > 0
                        else Decimal("0")
                    )
                    snapshot = PortfolioSnapshot(
                        total_usd=new_total,
                        stablecoin_balance=new_stablecoin,
                        open_position_count=open_count_new,
                        realized_pnl_today=daily_pnl.quantize(Decimal("0.00000001")),
                        peak_total_usd=new_peak,
                        current_drawdown_pct=drawdown,
                        snapshotted_at=now,
                    )
                    session.add(snapshot)
                    position_id = pos.id
        return position_id

    async def record_close(self, asset: str, result: ExecutionResult) -> bool:
        """Atomically close an open position after engine execution.

        Under the write lock:
          1. Look up the open position for the asset — return False if not found (no-op).
          2. Compute realized PnL from fill price vs entry.
          3. Update Position to closed, insert Trade and PortfolioSnapshot.

        Returns True if a position was closed, False if none was found.
        """
        async with self._lock:
            async with self._sf() as session:
                async with session.begin():
                    await self._acquire_write_lock(session)
                    pos_result = await session.execute(
                        select(Position)
                        .where(Position.asset == asset, Position.status == "open")
                        .limit(1)
                    )
                    pos = pos_result.scalar_one_or_none()
                    if pos is None:
                        return False

                    snap_row = await self._latest_snapshot(session)
                    daily_pnl = await self._sum_daily_pnl(session)

                    if snap_row is not None:
                        stablecoin = snap_row.stablecoin_balance
                        peak = snap_row.peak_total_usd
                        prev_total = snap_row.total_usd
                    else:
                        stablecoin = self._starting_balance
                        peak = self._starting_balance
                        prev_total = self._starting_balance

                    fill_price = result.executed_price
                    realized_pnl_usd = (
                        (fill_price - pos.entry_price) / pos.entry_price * pos.size_usd
                    ).quantize(Decimal("0.00000001"))
                    realized_pnl_pct = (
                        (fill_price - pos.entry_price) / pos.entry_price
                    ).quantize(Decimal("0.00000001"))

                    now = datetime.now(tz=timezone.utc)
                    open_count = await self._count_open_in_session(session)

                    pos.status = "closed"
                    pos.exit_price = fill_price
                    pos.exit_reason = "flat_intent"
                    pos.realized_pnl_usd = realized_pnl_usd
                    pos.realized_pnl_pct = realized_pnl_pct
                    pos.closed_at = now
                    pos.tx_hash_close = result.tx_hash

                    trade = Trade(
                        position_id=pos.id,
                        direction="close",
                        asset=asset,
                        price=fill_price,
                        size_usd=pos.size_usd,
                        slippage_pct=result.slippage_pct,
                        tx_hash=result.tx_hash,
                        executed_at=now,
                    )
                    session.add(trade)

                    new_stablecoin = (stablecoin + pos.size_usd + realized_pnl_usd).quantize(
                        Decimal("0.00000001")
                    )
                    new_total = (prev_total + realized_pnl_usd).quantize(Decimal("0.00000001"))
                    new_peak = max(peak, new_total)
                    drawdown = (
                        ((new_peak - new_total) / new_peak).quantize(Decimal("0.00000001"))
                        if new_peak > 0
                        else Decimal("0")
                    )
                    # open_count already reflects the closed position (pos.status updated above
                    # but not yet flushed — subtract 1 manually)
                    snapshot = PortfolioSnapshot(
                        total_usd=new_total,
                        stablecoin_balance=new_stablecoin,
                        open_position_count=max(0, open_count - 1),
                        realized_pnl_today=(daily_pnl + realized_pnl_usd).quantize(
                            Decimal("0.00000001")
                        ),
                        peak_total_usd=new_peak,
                        current_drawdown_pct=drawdown,
                        snapshotted_at=now,
                    )
                    session.add(snapshot)

        return True

    # ------------------------------------------------------------------
    # Private helpers
    # ------------------------------------------------------------------

    async def _fetch_open_positions(self, session: AsyncSession) -> list[Position]:
        result = await session.execute(
            select(Position).where(Position.status == "open")
        )
        return list(result.scalars().all())

    async def _latest_snapshot(self, session: AsyncSession) -> Optional[PortfolioSnapshot]:
        result = await session.execute(
            select(PortfolioSnapshot).order_by(PortfolioSnapshot.id.desc()).limit(1)
        )
        return result.scalar_one_or_none()

    async def _sum_daily_pnl(self, session: AsyncSession) -> Decimal:
        today_start = datetime.now(tz=timezone.utc).replace(
            hour=0, minute=0, second=0, microsecond=0
        )
        result = await session.execute(
            select(func.sum(Position.realized_pnl_usd)).where(
                Position.status == "closed",
                Position.closed_at >= today_start,
            )
        )
        total = result.scalar_one_or_none()
        return Decimal(str(total)) if total is not None else Decimal("0")

    async def _count_open_in_session(self, session: AsyncSession) -> int:
        result = await session.execute(
            select(func.count()).select_from(Position).where(Position.status == "open")
        )
        return result.scalar_one()

    async def _acquire_write_lock(self, session: AsyncSession) -> None:
        """Acquire a transaction-scoped advisory lock for portfolio writes.

        Uses pg_advisory_xact_lock on PostgreSQL — serializes all write transactions
        across every DB connection and process. Released automatically on commit/rollback.
        Skipped on SQLite (asyncio.Lock is sufficient for single-process tests).
        """
        dialect = session.bind.dialect.name if session.bind else ""
        if dialect == "postgresql":
            await session.execute(text("SELECT pg_advisory_xact_lock(1)"))

