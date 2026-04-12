from datetime import datetime
from decimal import Decimal
from typing import Literal

from pydantic import BaseModel, field_serializer

from common.models.trade_intent import DecimalField


class Position(BaseModel):
    # Identity
    id: int | None = None
    asset: str
    status: Literal["open", "closed"]
    action: Literal["LONG", "FLAT"]

    # Entry state
    entry_price: DecimalField
    size_usd: DecimalField
    size_pct: DecimalField
    stop_loss: DecimalField
    take_profit: DecimalField
    strategy: Literal["trend_following", "breakout", "mean_reversion"]
    trigger_reason: str
    reasoning: str
    opened_at: datetime
    tx_hash_open: str

    # Closed-state (None while open)
    closed_at: datetime | None = None
    exit_reason: Literal["take_profit", "stop_loss", "flat_intent"] | None = None
    exit_price: DecimalField | None = None
    realized_pnl_usd: DecimalField | None = None
    realized_pnl_pct: DecimalField | None = None
    tx_hash_close: str | None = None
    validation_uri: str | None = None

    @field_serializer(
        "entry_price", "size_usd", "size_pct", "stop_loss", "take_profit",
        "exit_price", "realized_pnl_usd", "realized_pnl_pct",
    )
    def _serialize_decimal(self, v: Decimal | None) -> str | None:
        return str(v) if v is not None else None
