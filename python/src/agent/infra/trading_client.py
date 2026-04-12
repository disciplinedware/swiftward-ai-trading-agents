from typing import Protocol, runtime_checkable

from common.models.portfolio_snapshot import PortfolioSnapshot
from common.models.trade_intent import TradeIntent


@runtime_checkable
class TradingClient(Protocol):
    """Common interface for Python and Go trading MCP clients."""

    async def health_check(self) -> bool: ...

    async def get_portfolio(self) -> PortfolioSnapshot: ...

    async def execute_swap(self, intent: TradeIntent) -> dict: ...
