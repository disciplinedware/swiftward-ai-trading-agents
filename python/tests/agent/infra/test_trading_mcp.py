from decimal import Decimal

import pytest
import respx
from httpx import Response

from agent.infra.trading_mcp import TradingMCPClient
from common.exceptions import MCPError
from common.models.trade_intent import TradeIntent

_BASE = "http://trading-mcp:8005"

_INTENT = TradeIntent(
    asset="ETH",
    action="LONG",
    size_pct=Decimal("0.1"),
    stop_loss=Decimal("2800"),
    take_profit=Decimal("3500"),
    strategy="trend_following",
    reasoning="stub://",
    trigger_reason="clock",
    confidence=0.8,
)

_PORTFOLIO_RESULT = {
    "total_usd": "10000",
    "stablecoin_balance": "9000",
    "open_position_count": 1,
    "realized_pnl_today": "50",
    "current_drawdown_pct": "0",
    "open_positions": [],
}


@pytest.mark.parametrize("name,method,params,result,call_fn", [
    (
        "get_portfolio sends no params",
        "get_portfolio",
        {},
        _PORTFOLIO_RESULT,
        lambda c: c.get_portfolio(),
    ),
    (
        "execute_swap sends intent dict",
        "execute_swap",
        None,  # checked loosely — intent dict is nested
        {
            "status": "executed", "tx_hash": "paper_abc",
            "executed_price": "3000", "slippage_pct": "0.001", "reason": None,
        },
        lambda c: c.execute_swap(_INTENT),
    ),
])
@respx.mock
async def test_rpc_payload_shape(name, method, params, result, call_fn):
    route = respx.post(f"{_BASE}/mcp").mock(
        return_value=Response(200, json={"jsonrpc": "2.0", "id": "1", "result": result})
    )
    client = TradingMCPClient(_BASE)
    await call_fn(client)

    req_json = route.calls[0].request.content
    import json
    body = json.loads(req_json)
    assert body["jsonrpc"] == "2.0"
    assert body["method"] == "tools/call"
    assert body["params"]["name"] == method
    if params is not None:
        assert body["params"]["arguments"] == params


@pytest.mark.parametrize("name,mock_response,expected_exc", [
    (
        "HTTP 500 raises MCPError",
        Response(500, text="Internal Server Error"),
        MCPError,
    ),
    (
        "JSON-RPC error body raises MCPError",
        Response(200, json={
            "jsonrpc": "2.0", "id": "1", "error": {"code": -32600, "message": "bad"},
        }),
        MCPError,
    ),
])
@respx.mock
async def test_rpc_errors(name, mock_response, expected_exc):
    respx.post(f"{_BASE}/mcp").mock(return_value=mock_response)
    client = TradingMCPClient(_BASE)
    with pytest.raises(expected_exc):
        await client.get_portfolio()


@respx.mock
async def test_health_check_returns_true_on_200():
    respx.get(f"{_BASE}/health").mock(return_value=Response(200, json={"status": "ok"}))
    client = TradingMCPClient(_BASE)
    assert await client.health_check() is True


@respx.mock
async def test_health_check_returns_false_on_error():
    respx.get(f"{_BASE}/health").mock(return_value=Response(503))
    client = TradingMCPClient(_BASE)
    assert await client.health_check() is False


async def test_health_check_returns_false_on_connection_error():
    import httpx
    mock_client = httpx.AsyncClient()
    client = TradingMCPClient("http://unreachable:9999", client=mock_client)
    # No respx mock — will get a connection error
    with respx.mock:
        respx.get("http://unreachable:9999/health").mock(side_effect=httpx.ConnectError("refused"))
        result = await client.health_check()
    assert result is False
