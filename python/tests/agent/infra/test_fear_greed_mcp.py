import pytest
import respx
from httpx import Response

from agent.infra.fear_greed_mcp import FearGreedMCPClient
from common.exceptions import MCPError
from common.models.signal_bundle import FearGreedData

_BASE = "http://fear-greed:8004"


@respx.mock
async def test_get_index_maps_to_fear_greed_data():
    respx.post(f"{_BASE}/mcp").mock(
        return_value=Response(200, json={
            "jsonrpc": "2.0", "id": "1",
            "result": {"value": 72, "classification": "Greed", "timestamp": "2026-03-25T00:00:00Z"},
        })
    )
    client = FearGreedMCPClient(_BASE)
    result = await client.get_index()

    assert isinstance(result, FearGreedData)
    assert result.value == 72
    assert result.classification == "Greed"
    assert result.timestamp == "2026-03-25T00:00:00Z"


@pytest.mark.parametrize("name,mock_resp", [
    ("HTTP 500", Response(500)),
    ("JSON-RPC error", Response(200, json={
        "jsonrpc": "2.0", "id": "1", "error": {"code": -1, "message": "err"},
    })),
])
@respx.mock
async def test_get_index_raises_mcp_error(name, mock_resp):
    respx.post(f"{_BASE}/mcp").mock(return_value=mock_resp)
    client = FearGreedMCPClient(_BASE)
    with pytest.raises(MCPError):
        await client.get_index()
