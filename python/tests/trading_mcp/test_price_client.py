"""Tests for PriceClient using respx to mock HTTP calls."""
from decimal import Decimal

import pytest
import respx
from httpx import Response

from common.exceptions import MCPError
from trading_mcp.infra.price_client import PriceClient


@pytest.fixture
def client():
    return PriceClient(base_url="http://price-feed-mcp:8001")


@pytest.mark.parametrize(
    "name,assets,mock_prices,expected",
    [
        (
            "multi-asset fetch",
            ["ETH", "BTC"],
            {"ETH": "3000.5", "BTC": "60000.0"},
            {"ETH": Decimal("3000.5"), "BTC": Decimal("60000.0")},
        ),
        (
            "single asset subset",
            ["SOL"],
            {"SOL": "150.25"},
            {"SOL": Decimal("150.25")},
        ),
    ],
)
@respx.mock
async def test_get_prices_latest_success(client, name, assets, mock_prices, expected):
    respx.post("http://price-feed-mcp:8001/mcp").mock(
        return_value=Response(
            200,
            json={"jsonrpc": "2.0", "id": 1, "result": mock_prices},
        )
    )
    result = await client.get_prices_latest(assets)
    assert result == expected, f"{name}: unexpected prices"


@respx.mock
async def test_get_price_single_asset(client):
    respx.post("http://price-feed-mcp:8001/mcp").mock(
        return_value=Response(
            200,
            json={"jsonrpc": "2.0", "id": 1, "result": {"ETH": "2500.00"}},
        )
    )
    price = await client.get_price("ETH")
    assert price == Decimal("2500.00")


@respx.mock
async def test_get_prices_latest_json_rpc_error_raises_mcp_error(client):
    respx.post("http://price-feed-mcp:8001/mcp").mock(
        return_value=Response(
            200,
            json={
                "jsonrpc": "2.0",
                "id": 1,
                "error": {"code": -32600, "message": "invalid request"},
            },
        )
    )
    with pytest.raises(MCPError, match="JSON-RPC error"):
        await client.get_prices_latest(["ETH"])


@respx.mock
async def test_get_price_missing_asset_raises_mcp_error(client):
    respx.post("http://price-feed-mcp:8001/mcp").mock(
        return_value=Response(
            200,
            json={"jsonrpc": "2.0", "id": 1, "result": {"BTC": "60000"}},
        )
    )
    with pytest.raises(MCPError, match="ETH"):
        await client.get_price("ETH")
