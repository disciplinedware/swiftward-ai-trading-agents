import random
from decimal import Decimal

from common.log import get_logger
from common.models.signal_bundle import SignalBundle
from common.models.trade_intent import TradeIntent

logger = get_logger(__name__)

_SIZE_PCT = Decimal("0.01")  # always spend 1% of stablecoin balance per trade
_ATR_STOP_MULT = Decimal("1.5")
_ATR_TARGET_MULT = Decimal("3.0")  # 2:1 R:R
_FALLBACK_STOP_PCT = Decimal("0.02")   # -2% when ATR unavailable
_FALLBACK_TARGET_PCT = Decimal("0.04")  # +4% when ATR unavailable


def _d(s: str) -> Decimal:
    try:
        return Decimal(s)
    except Exception:
        return Decimal("0")


def _stop_take(price: Decimal, atr: Decimal) -> tuple[Decimal, Decimal]:
    if atr > Decimal("0"):
        return price - atr * _ATR_STOP_MULT, price + atr * _ATR_TARGET_MULT
    return price * (1 - _FALLBACK_STOP_PCT), price * (1 + _FALLBACK_TARGET_PCT)


class StubBrain:
    async def run(self, signal_bundle: SignalBundle) -> list[TradeIntent]:
        candidates = [
            asset
            for asset, data in signal_bundle.prices.items()
            if _d(data.price) > Decimal("0")
        ]

        selected = random.sample(candidates, min(2, len(candidates)))
        logger.info("stub brain: selected assets", assets=selected)

        intents = []
        for asset in selected:
            data = signal_bundle.prices[asset]
            price = _d(data.price)
            stop_loss, take_profit = _stop_take(price, _d(data.atr_14_15m))
            intents.append(
                TradeIntent(
                    asset=asset,
                    action="LONG",
                    size_pct=_SIZE_PCT,
                    stop_loss=stop_loss,
                    take_profit=take_profit,
                    strategy="trend_following",
                    reasoning="stub brain: random asset selection, ATR-based SL/TP",
                    trigger_reason="clock",
                    confidence=0.5,
                )
            )
        return intents
