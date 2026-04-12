from datetime import datetime
from decimal import Decimal

from sqlalchemy import DateTime, ForeignKey, Integer, Numeric, Text
from sqlalchemy.orm import Mapped, mapped_column, relationship

from trading_mcp.domain.entity.base import Base

_PRICE = Numeric(20, 8)
_PCT = Numeric(10, 8)


class Trade(Base):
    __tablename__ = "trades"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    position_id: Mapped[int] = mapped_column(
        Integer, ForeignKey("positions.id"), nullable=False
    )
    direction: Mapped[str] = mapped_column(Text, nullable=False)  # "open" | "close"
    asset: Mapped[str] = mapped_column(Text, nullable=False)
    price: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    size_usd: Mapped[Decimal] = mapped_column(_PRICE, nullable=False)
    slippage_pct: Mapped[Decimal] = mapped_column(_PCT, nullable=False)
    tx_hash: Mapped[str] = mapped_column(Text, nullable=False)
    executed_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)

    position = relationship("Position", backref="trades")

    def __repr__(self) -> str:
        return (
            f"Trade(id={self.id}, position_id={self.position_id}, "
            f"direction={self.direction!r}, asset={self.asset!r})"
        )
