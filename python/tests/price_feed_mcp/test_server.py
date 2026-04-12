"""Tests for the MCP server layer — verifies delegation to PriceFeedService."""
from unittest.mock import AsyncMock

import pytest

import price_feed_mcp.server as srv
from price_feed_mcp.service.price_feed import PriceFeedService


@pytest.fixture(autouse=True)
def inject_service():
    mock_service = AsyncMock(spec=PriceFeedService)
    token = srv._service_var.set(mock_service)
    yield mock_service
    srv._service_var.reset(token)


async def test_get_prices_latest_delegates(inject_service):
    inject_service.get_prices_latest.return_value = {"BTC": "67432.15"}

    result = await srv.get_prices_latest(["BTC"])

    inject_service.get_prices_latest.assert_awaited_once_with(["BTC"])
    assert result == {"BTC": "67432.15"}


async def test_get_prices_change_delegates(inject_service):
    inject_service.get_prices_change.return_value = {"BTC": {"1m": "0.1"}}

    result = await srv.get_prices_change(["BTC"])

    inject_service.get_prices_change.assert_awaited_once_with(["BTC"])
    assert result == {"BTC": {"1m": "0.1"}}


async def test_get_indicators_delegates(inject_service):
    inject_service.get_indicators.return_value = {"BTC": {"rsi_14_15m": "55.0"}}

    result = await srv.get_indicators(["BTC"])

    inject_service.get_indicators.assert_awaited_once_with(["BTC"])
    assert result == {"BTC": {"rsi_14_15m": "55.0"}}


async def test_health_endpoint():
    from starlette.testclient import TestClient

    with TestClient(srv.mcp.streamable_http_app()) as client:
        resp = client.get("/health")
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}
