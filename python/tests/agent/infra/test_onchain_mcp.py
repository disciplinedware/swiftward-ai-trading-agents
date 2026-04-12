import pytest
import respx
from httpx import Response

from agent.infra.onchain_mcp import OnchainMCPClient
from common.exceptions import MCPError
from common.models.signal_bundle import OnchainData

_BASE = "http://onchain:8003"


def _mcp_ok(result):
    return Response(200, json={"jsonrpc": "2.0", "id": "1", "result": result})


@respx.mock
async def test_get_all_merges_parallel_calls():
    respx.post(f"{_BASE}/mcp").mock(side_effect=[
        _mcp_ok({"BTC": {
            "funding_rate": "0.0001", "annualized_pct": "10.95",
            "next_funding_time": "2026-03-25T08:00:00Z",
        }}),
        _mcp_ok({"BTC": {"oi_usd": "5000000000", "change_pct_24h": "-2.5"}}),
    ])
    client = OnchainMCPClient(_BASE)
    result = await client.get_all(["BTC"])

    assert "BTC" in result
    d = result["BTC"]
    assert isinstance(d, OnchainData)
    assert d.funding_rate == "0.0001"
    assert d.annualized_funding_pct == "10.95"
    assert d.oi_usd == "5000000000"
    assert d.oi_change_pct_24h == "-2.5"
    assert d.liquidated_usd_15m is None


@respx.mock
async def test_get_all_empty_assets_returns_empty():
    client = OnchainMCPClient(_BASE)
    result = await client.get_all([])
    assert result == {}


@pytest.mark.parametrize("name,mock_resp", [
    ("HTTP 500", Response(500)),
    ("JSON-RPC error", Response(200, json={
        "jsonrpc": "2.0", "id": "1", "error": {"code": -1, "message": "err"},
    })),
])
@respx.mock
async def test_get_all_raises_mcp_error(name, mock_resp):
    respx.post(f"{_BASE}/mcp").mock(return_value=mock_resp)
    client = OnchainMCPClient(_BASE)
    with pytest.raises(MCPError):
        await client.get_all(["BTC"])
