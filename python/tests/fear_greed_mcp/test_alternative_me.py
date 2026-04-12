"""Tests for AlternativeMeClient — HTTP parsing and error handling."""
import pytest
import respx
from httpx import Response

from common.exceptions import MCPError
from fear_greed_mcp.infra.alternative_me import AlternativeMeClient

_SAMPLE_ENTRY = {
    "value": "65",
    "value_classification": "Greed",
    "timestamp": "1742342400",
    "time_until_update": "3600",
}

_SAMPLE_RESPONSE = {"data": [_SAMPLE_ENTRY], "metadata": {"error": None}}


@pytest.fixture
async def client():
    c = AlternativeMeClient()
    await c.connect()
    yield c
    await c.close()


@pytest.mark.parametrize("method,kwargs,is_list", [
    ("get_index", {}, False),
    ("get_historical", {"limit": 1}, True),
])
@respx.mock
async def test_successful_parse(client, method, kwargs, is_list):
    """Infra layer returns raw API data (strings) — parsing is the service's job."""
    respx.get("https://api.alternative.me/fng/").mock(
        return_value=Response(200, json=_SAMPLE_RESPONSE)
    )

    result = await getattr(client, method)(**kwargs)

    entry = result[0] if is_list else result
    assert entry["value"] == "65"          # raw string from API
    assert entry["value_classification"] == "Greed"
    assert entry["timestamp"] == "1742342400"


@pytest.mark.parametrize("status_code", [429, 500, 503])
@respx.mock
async def test_http_error_raises_mcp_error(client, status_code):
    respx.get("https://api.alternative.me/fng/").mock(
        return_value=Response(status_code, text="error")
    )

    with pytest.raises(MCPError, match="Alternative.me error"):
        await client.get_index()


@respx.mock
async def test_invalid_json_raises_mcp_error(client):
    respx.get("https://api.alternative.me/fng/").mock(
        return_value=Response(200, content=b"not-json")
    )

    with pytest.raises(MCPError, match="invalid JSON"):
        await client.get_index()


@respx.mock
async def test_empty_data_raises_mcp_error(client):
    respx.get("https://api.alternative.me/fng/").mock(
        return_value=Response(200, json={"data": []})
    )

    with pytest.raises(MCPError, match="Unexpected"):
        await client.get_index()
