from datetime import datetime
from decimal import Decimal
from typing import Optional

from sqlalchemy import DateTime, Integer, Numeric, Text
from sqlalchemy.orm import Mapped, mapped_column

from trading_mcp.domain.entity.base import Base

_PRICE = Numeric(20, 8)
_PCT = Numeric(10, 8)


class Position(Base):
    __tablename__ = "positions"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    asset: Mapped[str] = mapped_column(Text, nullable=False)
    status: Mapped[str] = mapped_column(Text, nullable=False)  # "open" | "closed"
    action: Mapped[str] = mapped_column(Text, nullable=False)  # "LONG" | "FLAT"
    entry_price: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    size_usd: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    size_pct: Mapped[Decimal] = mapped_column(_PCT, nullable=False)
    stop_loss: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    take_profit: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    strategy: Mapped[str] = mapped_column(Text, nullable=False)
    trigger_reason: Mapped[str] = mapped_column(Text, nullable=False)
    reasoning: Mapped[str] = mapped_column(Text, nullable=False)
    validation_uri: Mapped[Optional[str]] = mapped_column(Text, nullable=True)
    opened_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)
    closed_at: Mapped[Optional[datetime]] = mapped_column(DateTime(timezone=True), nullable=True)
    exit_reason: Mapped[Optional[str]] = mapped_column(Text, nullable=True)
    exit_price: Mapped[Optional[Decimal]] = mapped_column(_PRICE, nullable=True)
    realized_pnl_usd: Mapped[Optional[Decimal]] = mapped_column(_PRICE, nullable=True)
    realized_pnl_pct: Mapped[Optional[Decimal]] = mapped_column(_PCT, nullable=True)
    tx_hash_open: Mapped[str] = mapped_column(Text, nullable=False)
    tx_hash_close: Mapped[Optional[str]] = mapped_column(Text, nullable=True)

    def __repr__(self) -> str:
        return (
            f"Position(id={self.id}, asset={self.asset!r}, "
            f"status={self.status!r}, action={self.action!r})"
        )
