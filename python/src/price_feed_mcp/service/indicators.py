from decimal import Decimal

import pandas as pd
import pandas_ta as ta  # noqa: F401 — registers DataFrame.ta accessor


def _klines_to_df(klines: list[list]) -> pd.DataFrame:
    """Convert Binance kline list to OHLCV DataFrame."""
    df = pd.DataFrame(
        klines,
        columns=[
            "open_time", "open", "high", "low", "close", "volume",
            "close_time", "quote_volume", "trades",
            "taker_buy_base", "taker_buy_quote", "ignore",
        ],
    )
    for col in ("open", "high", "low", "close", "volume"):
        df[col] = df[col].astype(float)
    return df


def _last(series: pd.Series) -> Decimal:
    """Return last non-NaN value as Decimal."""
    val = series.dropna().iloc[-1]
    return Decimal(str(round(float(val), 8)))


def compute_indicators(klines_15m: list[list], klines_1h: list[list]) -> dict:
    """Compute all required technical indicators using pandas-ta.

    Returns a flat dict of indicator name -> str-encoded Decimal (or float for volume_ratio).
    """
    df15 = _klines_to_df(klines_15m)
    df1h = _klines_to_df(klines_1h)

    # RSI 14
    rsi = df15.ta.rsi(length=14)

    # EMA 20 / 50 on 15m
    ema20 = df15.ta.ema(length=20)
    ema50 = df15.ta.ema(length=50)

    # EMA 50 / 200 on 1h
    ema50_1h = df1h.ta.ema(length=50)
    ema200 = df1h.ta.ema(length=200)

    # ATR 14 on 15m
    atr = df15.ta.atr(length=14)
    atr_clean = atr.dropna()
    if len(atr_clean) < 6:
        raise ValueError(f"Insufficient ATR data: need ≥6 values, got {len(atr_clean)}")
    atr_chg_5 = (atr_clean.iloc[-1] - atr_clean.iloc[-6]) / atr_clean.iloc[-6] * 100

    # Bollinger Bands 20 on 15m
    bb = df15.ta.bbands(length=20)

    # Volume ratio: last candle volume / 20-period average
    vol_avg = df15["volume"].rolling(20).mean()
    vol_ratio = df15["volume"].iloc[-1] / vol_avg.iloc[-1]

    # Column names vary by pandas-ta version; find them dynamically
    bb_upper_col = next(c for c in bb.columns if c.startswith("BBU_"))
    bb_mid_col = next(c for c in bb.columns if c.startswith("BBM_"))
    bb_lower_col = next(c for c in bb.columns if c.startswith("BBL_"))

    return {
        "rsi_14_15m": str(_last(rsi)),
        "ema_20_15m": str(_last(ema20)),
        "ema_50_15m": str(_last(ema50)),
        "ema_50_1h": str(_last(ema50_1h)),
        "ema_200_1h": str(_last(ema200)),
        "atr_14_15m": str(_last(atr)),
        "atr_chg_5": str(Decimal(str(round(float(atr_chg_5), 1)))),
        "bb_upper_15m": str(_last(bb[bb_upper_col])),
        "bb_mid_15m": str(_last(bb[bb_mid_col])),
        "bb_lower_15m": str(_last(bb[bb_lower_col])),
        "volume_ratio_15m": str(Decimal(str(round(float(vol_ratio), 6)))),
    }
