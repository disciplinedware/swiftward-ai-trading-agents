import dataclasses
from dataclasses import dataclass, field
from datetime import datetime
from decimal import Decimal
from typing import Any, Optional


@dataclass
class PositionView:
    id: int
    asset: str
    status: str
    action: str
    entry_price: Decimal
    size_usd: Decimal
    size_pct: Decimal
    stop_loss: Decimal
    take_profit: Decimal
    strategy: str
    trigger_reason: str
    reasoning: str
    opened_at: datetime
    tx_hash_open: str
    unrealized_pnl_usd: Decimal
    unrealized_pnl_pct: Decimal
    current_price: Decimal
    validation_uri: Optional[str] = None
    closed_at: Optional[datetime] = None
    exit_reason: Optional[str] = None
    exit_price: Optional[Decimal] = None
    realized_pnl_usd: Optional[Decimal] = None
    realized_pnl_pct: Optional[Decimal] = None
    tx_hash_close: Optional[str] = None

    _DECIMAL_FIELDS = {
        "entry_price", "size_usd", "size_pct", "stop_loss", "take_profit",
        "unrealized_pnl_usd", "unrealized_pnl_pct", "current_price",
        "exit_price", "realized_pnl_usd", "realized_pnl_pct",
    }

    def to_dict(self) -> dict[str, Any]:
        d = dataclasses.asdict(self)
        for f in self._DECIMAL_FIELDS:
            if d.get(f) is not None:
                d[f] = str(d[f])
        for f in ("opened_at", "closed_at"):
            if d.get(f) is not None:
                d[f] = d[f].isoformat()
        return d


@dataclass
class PortfolioSummary:
    total_usd: Decimal
    stablecoin_balance: Decimal
    open_position_count: int
    realized_pnl_today: Decimal
    current_drawdown_pct: Decimal
    peak_total_usd: Decimal
    open_positions: list[PositionView] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "total_usd": str(self.total_usd),
            "stablecoin_balance": str(self.stablecoin_balance),
            "open_position_count": self.open_position_count,
            "realized_pnl_today": str(self.realized_pnl_today),
            "current_drawdown_pct": str(self.current_drawdown_pct),
            "peak_total_usd": str(self.peak_total_usd),
            "open_positions": [p.to_dict() for p in self.open_positions],
        }
