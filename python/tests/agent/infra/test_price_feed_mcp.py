import pytest
import respx
from httpx import Response

from agent.infra.price_feed_mcp import PriceFeedMCPClient
from common.exceptions import MCPError
from common.models.signal_bundle import PriceFeedData

_BASE = "http://price-feed:8001"


def _mcp_ok(result):
    return Response(200, json={"jsonrpc": "2.0", "id": "1", "result": result})


@respx.mock
async def test_get_prices_maps_to_price_feed_data():
    # Three tools are called: get_prices_latest, get_prices_change, get_indicators
    respx.post(f"{_BASE}/mcp").mock(side_effect=[
        _mcp_ok({"BTC": "95000"}),  # get_prices_latest
        _mcp_ok({"BTC": {"1m": "0.1", "5m": "0.2", "1h": "0.5", "4h": "1.0", "24h": "2.0"}}),
        _mcp_ok({"BTC": {
            "rsi_14_15m": "65.0", "ema_20_15m": "90000", "ema_50_15m": "85000",
            "ema_50_1h": "82000", "ema_200_1h": "80000",
            "atr_14_15m": "2000", "bb_upper_15m": "98000",
            "bb_mid_15m": "95000", "bb_lower_15m": "92000", "volume_ratio_15m": "1.5",
        }}),
    ])
    client = PriceFeedMCPClient(_BASE)
    result = await client.get_prices(["BTC"])

    assert "BTC" in result
    d = result["BTC"]
    assert isinstance(d, PriceFeedData)
    assert d.price == "95000"
    assert d.change_1m == "0.1"
    assert d.change_24h == "2.0"
    assert d.rsi_14_15m == "65.0"
    assert d.ema_50_1h == "82000"
    assert d.ema_200_1h == "80000"
    assert d.volume_ratio_15m == "1.5"


@respx.mock
async def test_get_prices_empty_assets_returns_empty():
    client = PriceFeedMCPClient(_BASE)
    result = await client.get_prices([])
    assert result == {}


@pytest.mark.parametrize("name,mock_resp", [
    ("HTTP 500", Response(500)),
    ("JSON-RPC error", Response(200, json={
        "jsonrpc": "2.0", "id": "1", "error": {"code": -1, "message": "err"},
    })),
])
@respx.mock
async def test_get_prices_raises_mcp_error(name, mock_resp):
    respx.post(f"{_BASE}/mcp").mock(return_value=mock_resp)
    client = PriceFeedMCPClient(_BASE)
    with pytest.raises(MCPError):
        await client.get_prices(["BTC"])
