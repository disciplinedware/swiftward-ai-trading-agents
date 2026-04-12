from decimal import Decimal

import httpx
import pytest
import respx

from common.exceptions import MCPError
from price_feed_mcp.infra.binance import BinanceClient, asset_to_symbol


@pytest.mark.parametrize(
    "asset, expected_symbol",
    [
        ("BTC", "BTCUSDT"),
        ("ETH", "ETHUSDT"),
        ("SOL", "SOLUSDT"),
        ("AVAX", "AVAXUSDT"),
    ],
)
def test_asset_to_symbol(asset, expected_symbol):
    assert asset_to_symbol(asset) == expected_symbol


@respx.mock
async def test_get_ticker_price_success():
    respx.get("https://api.binance.com/api/v3/ticker/price").mock(
        return_value=httpx.Response(200, json={"symbol": "BTCUSDT", "price": "67432.15000000"})
    )
    client = BinanceClient()
    await client.connect()
    price = await client.get_ticker_price("BTCUSDT")
    await client.close()
    assert price == Decimal("67432.15000000")


@respx.mock
async def test_get_ticker_price_binance_error_raises_mcp_error():
    respx.get("https://api.binance.com/api/v3/ticker/price").mock(
        return_value=httpx.Response(400, json={"code": -1121, "msg": "Invalid symbol."})
    )
    client = BinanceClient()
    await client.connect()
    with pytest.raises(MCPError, match="Binance error 400"):
        await client.get_ticker_price("FAKEUSDT")
    await client.close()


@respx.mock
async def test_get_ticker_price_network_error_raises_mcp_error():
    respx.get("https://api.binance.com/api/v3/ticker/price").mock(
        side_effect=httpx.ConnectError("connection refused")
    )
    client = BinanceClient()
    await client.connect()
    with pytest.raises(MCPError, match="Binance request failed"):
        await client.get_ticker_price("BTCUSDT")
    await client.close()


@respx.mock
async def test_get_klines_returns_list():
    fake_kline = [
        1700000000000, "67000.0", "67500.0", "66800.0", "67200.0",
        "12.5", 1700000059999, "838500.0", 150,
        "6.2", "416100.0", "0",
    ]
    respx.get("https://api.binance.com/api/v3/klines").mock(
        return_value=httpx.Response(200, json=[fake_kline, fake_kline])
    )
    client = BinanceClient()
    await client.connect()
    klines = await client.get_klines("BTCUSDT", "15m", 2)
    await client.close()
    assert len(klines) == 2
    assert klines[0][4] == "67200.0"  # close price
