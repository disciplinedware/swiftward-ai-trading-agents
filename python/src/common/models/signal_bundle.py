from pydantic import BaseModel

from common.models.portfolio_snapshot import PortfolioSnapshot
from common.models.trade_intent import TriggerReason


class PriceFeedData(BaseModel):
    price: str = "0"
    change_1m: str = "0"
    change_5m: str = "0"
    change_1h: str = "0"
    change_4h: str = "0"
    change_24h: str = "0"
    rsi_14_15m: str = "0"
    ema_20_15m: str = "0"
    ema_50_15m: str = "0"
    ema_50_1h: str = "0"
    ema_200_1h: str = "0"
    atr_14_15m: str = "0"
    atr_chg_5: str = "0"
    bb_upper_15m: str = "0"
    bb_mid_15m: str = "0"
    bb_lower_15m: str = "0"
    volume_ratio_15m: str = "1"


class FearGreedData(BaseModel):
    value: int = 50
    classification: str = "Neutral"
    timestamp: str = ""


class OnchainData(BaseModel):
    funding_rate: str | None = None
    annualized_funding_pct: str | None = None
    next_funding_time: str | None = None
    oi_usd: str | None = None
    oi_change_pct_24h: str | None = None
    liquidated_usd_15m: str | None = None
    long_liquidated_usd: str | None = None
    short_liquidated_usd: str | None = None


class NewsData(BaseModel):
    sentiment: float = 0.0
    macro_flag: bool = False


class SignalBundle(BaseModel):
    prices: dict[str, PriceFeedData]
    fear_greed: FearGreedData
    onchain: dict[str, OnchainData]
    news: dict[str, NewsData]
    portfolio: PortfolioSnapshot
    trigger_reason: TriggerReason
