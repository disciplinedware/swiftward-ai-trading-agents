from decimal import Decimal

from pydantic import BaseModel, field_serializer

from common.models.trade_intent import DecimalField


class OpenPositionView(BaseModel):
    asset: str
    entry_price: DecimalField
    stop_loss: DecimalField
    take_profit: DecimalField
    size_pct: DecimalField
    strategy: str

    @field_serializer("entry_price", "stop_loss", "take_profit", "size_pct")
    def _serialize_decimal(self, v: Decimal) -> str:
        return str(v)


class PortfolioSnapshot(BaseModel):
    total_usd: DecimalField
    stablecoin_balance: DecimalField
    open_position_count: int
    realized_pnl_today: DecimalField
    current_drawdown_pct: DecimalField
    open_positions: list[OpenPositionView] = []

    @field_serializer(
        "total_usd", "stablecoin_balance", "realized_pnl_today", "current_drawdown_pct"
    )
    def _serialize_decimal(self, v: Decimal) -> str:
        return str(v)
