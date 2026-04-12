import pytest
import respx
from httpx import Response

from agent.infra.news_mcp import NewsMCPClient
from common.exceptions import MCPError
from common.models.signal_bundle import NewsData

_BASE = "http://news:8002"


def _mcp_ok(result):
    return Response(200, json={"jsonrpc": "2.0", "id": "1", "result": result})


@respx.mock
async def test_get_signals_merges_sentiment_and_macro():
    respx.post(f"{_BASE}/mcp").mock(side_effect=[
        _mcp_ok({"BTC": 0.7, "ETH": -0.2}),  # get_sentiment
        _mcp_ok({"triggered": True, "reason": "Fed rate decision"}),  # get_macro_flag
    ])
    client = NewsMCPClient(_BASE)
    result = await client.get_signals(["BTC", "ETH"])

    assert "BTC" in result
    assert "ETH" in result
    assert isinstance(result["BTC"], NewsData)
    assert result["BTC"].sentiment == 0.7
    assert result["BTC"].macro_flag is True
    assert result["ETH"].sentiment == -0.2
    assert result["ETH"].macro_flag is True  # macro flag is global


@respx.mock
async def test_get_signals_empty_assets_returns_empty():
    client = NewsMCPClient(_BASE)
    result = await client.get_signals([])
    assert result == {}


@pytest.mark.parametrize("name,mock_resp", [
    ("HTTP 500", Response(500)),
    ("JSON-RPC error", Response(200, json={
        "jsonrpc": "2.0", "id": "1", "error": {"code": -1, "message": "err"},
    })),
])
@respx.mock
async def test_get_signals_raises_mcp_error(name, mock_resp):
    respx.post(f"{_BASE}/mcp").mock(return_value=mock_resp)
    client = NewsMCPClient(_BASE)
    with pytest.raises(MCPError):
        await client.get_signals(["BTC"])
