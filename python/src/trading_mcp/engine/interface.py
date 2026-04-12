from dataclasses import dataclass, field
from decimal import Decimal
from typing import Any, Protocol, runtime_checkable

from common.models.trade_intent import TradeIntent


@dataclass
class ExecutionResult:
    status: str       # "executed" | "rejected"
    tx_hash: str      # "paper_<uuid4>" on success, "" on rejection
    executed_price: Decimal
    slippage_pct: Decimal
    size_usd: Decimal  # USD value transacted; 0 on rejection
    intent_hash: bytes  # EIP-712 intent digest (32 bytes); ZERO_BYTES32 for paper
    reason: str = field(default="")  # empty on success, rejection reason on reject

    def to_dict(self) -> dict[str, Any]:
        return {
            "status": self.status,
            "tx_hash": self.tx_hash,
            "executed_price": str(self.executed_price),
            "slippage_pct": str(self.slippage_pct),
            "size_usd": str(self.size_usd),
            "reason": self.reason,
        }


@runtime_checkable
class Engine(Protocol):
    """Execution engine contract — implemented by PaperEngine and LiveEngine."""

    async def execute_swap(
        self,
        intent: TradeIntent,
        current_price: Decimal,
        amount_usd: Decimal,
    ) -> ExecutionResult: ...
