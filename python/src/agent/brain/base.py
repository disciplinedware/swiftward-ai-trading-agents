from typing import Protocol, runtime_checkable

from common.models.signal_bundle import SignalBundle
from common.models.trade_intent import TradeIntent


@runtime_checkable
class Brain(Protocol):
    async def run(self, signal_bundle: SignalBundle) -> list[TradeIntent]: ...
