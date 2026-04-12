"""Tests for PriceFeedService — business logic: caching, price change, indicators."""
from decimal import Decimal
from unittest.mock import AsyncMock

import pytest

from common.cache import RedisCache
from price_feed_mcp.infra.binance import BinanceClient
from price_feed_mcp.service.price_feed import PriceFeedService, _pct_change

# ---------------------------------------------------------------------------
# Fake kline factory
# ---------------------------------------------------------------------------

_KLINE_TEMPLATE = [
    1700000000000, "67000.0", "67500.0", "66800.0", "67200.0",
    "12.5", 1700000059999, "838500.0", 150, "6.2", "416100.0", "0",
]


def make_klines(n: int, close_prices: list[float] | None = None) -> list[list]:
    klines = []
    for i in range(n):
        k = list(_KLINE_TEMPLATE)
        if close_prices and i < len(close_prices):
            k[4] = str(close_prices[i])
        k[5] = str(12.5 + i)
        klines.append(k)
    return klines


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def deps():
    binance = AsyncMock(spec=BinanceClient)
    cache = AsyncMock(spec=RedisCache)
    cache.get = AsyncMock(return_value=None)
    cache.set = AsyncMock()
    return PriceFeedService(binance, cache), binance, cache


# ---------------------------------------------------------------------------
# get_prices_latest
# ---------------------------------------------------------------------------

async def test_get_prices_latest_returns_price_strings(deps):
    svc, binance, _ = deps
    binance.get_ticker_price = AsyncMock(return_value=Decimal("67432.15"))

    result = await svc.get_prices_latest(["BTC", "ETH"])

    assert set(result.keys()) == {"BTC", "ETH"}
    assert result["BTC"] == "67432.15"


async def test_get_prices_latest_cache_hit_skips_binance(deps):
    svc, binance, cache = deps
    cache.get = AsyncMock(return_value={"price": "67000.0"})

    result = await svc.get_prices_latest(["BTC"])

    assert result["BTC"] == "67000.0"
    binance.get_ticker_price.assert_not_called()


# ---------------------------------------------------------------------------
# get_prices_change
# ---------------------------------------------------------------------------

async def test_get_prices_change_returns_all_windows(deps):
    svc, binance, _ = deps
    binance.get_klines = AsyncMock(side_effect=lambda symbol, interval, limit: (
        make_klines(2, [67000.0, 67200.0]) if interval in ("1m", "5m")
        else make_klines(250, [67000.0 + i for i in range(250)])
    ))

    result = await svc.get_prices_change(["BTC"])

    assert set(result["BTC"].keys()) == {"1m", "5m", "1h", "4h", "24h"}
    for v in result["BTC"].values():
        float(v)  # must be parseable


# ---------------------------------------------------------------------------
# get_indicators
# ---------------------------------------------------------------------------

async def test_get_indicators_returns_all_keys(deps):
    svc, binance, _ = deps
    binance.get_klines = AsyncMock(
        return_value=make_klines(250, [67000.0 + i * 0.5 for i in range(250)])
    )

    result = await svc.get_indicators(["BTC"])

    expected = {
        "rsi_14_15m", "ema_20_15m", "ema_50_15m", "ema_50_1h", "ema_200_1h",
        "atr_14_15m", "atr_chg_5", "bb_upper_15m", "bb_mid_15m", "bb_lower_15m",
        "volume_ratio_15m",
    }
    assert set(result["BTC"].keys()) == expected
    for v in result["BTC"].values():
        float(v)


# ---------------------------------------------------------------------------
# _pct_change
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("closes,lookback,expected", [
    ([100.0, 110.0], 1, "10.0"),       # +10%
    ([110.0, 99.0], 1, "-10.0"),       # -~10%  (rounded)
    ([100.0, 100.0], 1, "0.0"),        # flat
    ([0.0, 100.0], 1, "0"),            # zero prev → guard
])
def test_pct_change(closes, lookback, expected):
    klines = make_klines(len(closes), closes)
    result = _pct_change(klines, lookback=lookback)
    assert float(result) == pytest.approx(float(expected), rel=1e-4)
