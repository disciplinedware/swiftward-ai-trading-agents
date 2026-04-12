from decimal import Decimal
from typing import Annotated, Any, Literal

from pydantic import BaseModel, BeforeValidator, field_serializer, model_validator


def _to_decimal(v: Any) -> Decimal:
    if isinstance(v, Decimal):
        return v
    return Decimal(str(v))


def _to_decimal_opt(v: Any) -> Decimal | None:
    if v is None:
        return None
    if isinstance(v, Decimal):
        return v
    return Decimal(str(v))


DecimalField = Annotated[Decimal, BeforeValidator(_to_decimal)]
DecimalFieldOpt = Annotated[Decimal | None, BeforeValidator(_to_decimal_opt)]

StrategyTag = Literal["trend_following", "breakout", "mean_reversion"]
TriggerReason = Literal[
    "clock", "price_spike", "stop_loss", "take_profit", "news", "liquidation", "fear_greed"
]


class TradeIntent(BaseModel):
    asset: str | None = None
    action: Literal["LONG", "FLAT", "FLAT_ALL"]
    size_pct: DecimalField  # fraction of stablecoin balance, e.g. 0.01 = 1%
    stop_loss: DecimalFieldOpt = None
    take_profit: DecimalFieldOpt = None
    strategy: StrategyTag
    reasoning: str  # plain-text rationale; trading MCP embeds it in the IPFS validation trace
    trigger_reason: TriggerReason
    confidence: float  # 0.0–1.0; brain's confidence in the decision

    @model_validator(mode="after")
    def _validate_fields_per_action(self) -> "TradeIntent":
        if self.action == "LONG":
            if self.asset is None:
                raise ValueError("LONG intent requires 'asset'")
            if self.stop_loss is None:
                raise ValueError("LONG intent requires 'stop_loss'")
            if self.take_profit is None:
                raise ValueError("LONG intent requires 'take_profit'")
        elif self.action == "FLAT":
            if self.asset is None:
                raise ValueError("FLAT intent requires 'asset'")
        elif self.action == "FLAT_ALL":
            if self.asset is not None:
                raise ValueError("FLAT_ALL intent must have asset=None")
            if self.stop_loss is not None:
                raise ValueError("FLAT_ALL intent must have stop_loss=None")
            if self.take_profit is not None:
                raise ValueError("FLAT_ALL intent must have take_profit=None")
        return self

    @field_serializer("size_pct")
    def _serialize_size_pct(self, v: Decimal) -> str:
        return str(v)

    @field_serializer("stop_loss", "take_profit")
    def _serialize_decimal_opt(self, v: Decimal | None) -> str | None:
        return str(v) if v is not None else None
