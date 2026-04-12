"""Tests for OnchainDataService — funding rate math, OI change, liquidation aggregation, netflow."""
from unittest.mock import AsyncMock

import pytest

from common.cache import RedisCache
from onchain_data_mcp.infra.binance_futures import BinanceFuturesClient
from onchain_data_mcp.service.onchain_data import OnchainDataService

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_service(*, cached=None):
    client = AsyncMock(spec=BinanceFuturesClient)
    cache = AsyncMock(spec=RedisCache)
    cache.get.return_value = cached
    cache.set = AsyncMock()
    return OnchainDataService(client, cache), client, cache


# ---------------------------------------------------------------------------
# get_funding_rate
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,last_funding_rate,expected_rate,expected_annualized", [
    (
        "positive funding rate",
        "0.00010000",
        "0.0001",    # normalize() strips trailing zeros
        "10.95",     # 0.0001 × 3 × 365 × 100
    ),
    (
        "negative funding rate",
        "-0.00030000",
        "-0.0003",   # normalize() strips trailing zeros
        "-32.85",    # -0.0003 × 3 × 365 × 100
    ),
    (
        "zero funding rate",
        "0.00000000",
        "0",
        "0",
    ),
])
async def test_get_funding_rate(name, last_funding_rate, expected_rate, expected_annualized):
    svc, client, cache = _make_service()
    client.get_premium_index.return_value = {
        "lastFundingRate": last_funding_rate,
        "nextFundingTime": 1742400000000,
    }

    result = await svc.get_funding_rate(["BTC"])

    assert "BTC" in result, name
    assert result["BTC"]["funding_rate"] == expected_rate, name
    assert result["BTC"]["annualized_pct"] == expected_annualized, name
    assert "next_funding_time" in result["BTC"], name
    cache.set.assert_called_once()


async def test_get_funding_rate_cache_hit():
    cached_entry = {
        "funding_rate": "0.0001",
        "annualized_pct": "10.95",
        "next_funding_time": "2026-03-20T08:00:00Z",
    }
    svc, client, cache = _make_service(cached=cached_entry)

    result = await svc.get_funding_rate(["BTC"])

    assert result["BTC"] == cached_entry
    client.get_premium_index.assert_not_called()
    cache.set.assert_not_called()


# ---------------------------------------------------------------------------
# get_open_interest
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,prev_oi,latest_oi,expected_change_pct", [
    (
        "OI increasing",
        "10000000",
        "11200000",
        "12.00",
    ),
    (
        "OI decreasing",
        "10000000",
        "9500000",
        "-5.00",
    ),
    (
        "OI unchanged",
        "10000000",
        "10000000",
        "0.00",
    ),
])
async def test_get_open_interest(name, prev_oi, latest_oi, expected_change_pct):
    svc, client, cache = _make_service()
    client.get_open_interest_hist.return_value = [
        {"sumOpenInterestValue": prev_oi, "timestamp": 1742313600000},
        {"sumOpenInterestValue": latest_oi, "timestamp": 1742400000000},
    ]

    result = await svc.get_open_interest(["BTC"])

    assert "BTC" in result, name
    assert result["BTC"]["oi_usd"] == latest_oi, name
    assert result["BTC"]["change_pct_24h"] == expected_change_pct, name


# ---------------------------------------------------------------------------
# get_liquidations
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,orders,expected_total,expected_long,expected_short", [
    (
        "mixed long and short liquidations",
        [
            {"side": "SELL", "avgPrice": "50000", "executedQty": "0.064"},   # long liq: $3200
            {"side": "BUY",  "avgPrice": "50000", "executedQty": "0.026"},   # short liq: $1300
        ],
        "4500.00",
        "3200.00",
        "1300.00",
    ),
    (
        "only long liquidations",
        [
            {"side": "SELL", "avgPrice": "60000", "executedQty": "0.1"},
        ],
        "6000.00",
        "6000.00",
        "0.00",
    ),
    (
        "no liquidations",
        [],
        "0.00",
        "0.00",
        "0.00",
    ),
])
async def test_get_liquidations(name, orders, expected_total, expected_long, expected_short):
    svc, client, cache = _make_service()
    client.get_force_orders.return_value = orders

    result = await svc.get_liquidations(["BTC"])

    assert "BTC" in result, name
    assert result["BTC"]["liquidated_usd_15m"] == expected_total, name
    assert result["BTC"]["long_liquidated_usd"] == expected_long, name
    assert result["BTC"]["short_liquidated_usd"] == expected_short, name


# ---------------------------------------------------------------------------
# get_netflow
# ---------------------------------------------------------------------------

async def test_get_netflow_returns_neutral_for_btc_and_eth():
    svc, client, cache = _make_service()

    result = await svc.get_netflow()

    assert "BTC" in result
    assert "ETH" in result
    assert result["BTC"]["direction"] == "neutral"
    assert result["ETH"]["direction"] == "neutral"


async def test_get_netflow_makes_no_http_call():
    svc, client, cache = _make_service()

    await svc.get_netflow()

    client.get_premium_index.assert_not_called()
    client.get_open_interest_hist.assert_not_called()
    client.get_force_orders.assert_not_called()
    cache.get.assert_not_called()
    cache.set.assert_not_called()
