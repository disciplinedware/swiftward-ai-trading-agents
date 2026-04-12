from datetime import datetime
from decimal import Decimal

from sqlalchemy import DateTime, Integer, Numeric
from sqlalchemy.orm import Mapped, mapped_column

from trading_mcp.domain.entity.base import Base

_PRICE = Numeric(20, 8)
_PCT = Numeric(10, 8)


class PortfolioSnapshot(Base):
    __tablename__ = "portfolio_snapshots"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    total_usd: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    stablecoin_balance: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    open_position_count: Mapped[int] = mapped_column(Integer, nullable=False)
    realized_pnl_today: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    peak_total_usd: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    current_drawdown_pct: Mapped[Decimal] = mapped_column(_PCT, nullable=False)
    snapshotted_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)

    def __repr__(self) -> str:
        return (
            f"PortfolioSnapshot(id={self.id}, total_usd={self.total_usd}, "
            f"snapshotted_at={self.snapshotted_at})"
        )
