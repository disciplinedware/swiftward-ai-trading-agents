import uuid
from decimal import Decimal

from common.models.trade_intent import TradeIntent
from trading_mcp.engine.interface import Engine, ExecutionResult

_SLIPPAGE = Decimal("0.001")  # 0.1%
_ZERO_BYTES32 = b"\x00" * 32


class PaperEngine(Engine):
    """Paper execution engine — pure fill-price simulation, no DB access.

    Fills LONG at current_price × (1 + slippage).
    Fills FLAT at exactly current_price (no slippage on exits).
    All portfolio accounting (DB writes, capacity checks) is handled by
    PortfolioService after this call returns.
    """

    async def execute_swap(
        self,
        intent: TradeIntent,
        current_price: Decimal,
        amount_usd: Decimal,
    ) -> ExecutionResult:
        if intent.action == "LONG":
            return self._fill_long(current_price, amount_usd)
        if intent.action == "FLAT":
            return self._fill_flat(current_price, amount_usd)
        return ExecutionResult(
            status="rejected",
            tx_hash="",
            executed_price=current_price,
            slippage_pct=Decimal("0"),
            size_usd=Decimal("0"),
            intent_hash=_ZERO_BYTES32,
            reason=f"Unknown action: {intent.action!r}",
        )

    def _fill_long(self, current_price: Decimal, amount_usd: Decimal) -> ExecutionResult:
        fill_price = (current_price * (1 + _SLIPPAGE)).quantize(Decimal("0.00000001"))
        return ExecutionResult(
            status="executed",
            tx_hash=f"paper_{uuid.uuid4().hex}",
            executed_price=fill_price,
            slippage_pct=_SLIPPAGE,
            size_usd=amount_usd,
            intent_hash=_ZERO_BYTES32,
        )

    def _fill_flat(self, current_price: Decimal, amount_usd: Decimal) -> ExecutionResult:
        fill_price = (current_price * (1 - _SLIPPAGE)).quantize(Decimal("0.00000001"))
        return ExecutionResult(
            status="executed",
            tx_hash=f"paper_{uuid.uuid4().hex}",
            executed_price=fill_price,
            slippage_pct=_SLIPPAGE,
            size_usd=amount_usd,
            intent_hash=_ZERO_BYTES32,
        )
