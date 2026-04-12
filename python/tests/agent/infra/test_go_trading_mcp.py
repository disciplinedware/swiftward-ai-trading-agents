"""Tests for GoTradingMCPClient - Go Trading MCP adapter."""

import json
from decimal import Decimal

import pytest
import respx
from httpx import Response

from agent.infra.go_trading_mcp import GoTradingMCPClient
from common.exceptions import MCPError
from common.models.trade_intent import TradeIntent

_BASE = "http://go-trading:8091"
_AGENT_ID = "agent-test-001"
_MCP_URL = f"{_BASE}/mcp/trading"

# Helpers to build mock Go MCP responses (content-wrapped JSON text).

def _mcp_ok(payload: dict) -> Response:
    """Wrap a dict as a Go MCP text-content response."""
    return Response(
        200,
        json={
            "jsonrpc": "2.0",
            "id": "1",
            "result": {
                "content": [{"type": "text", "text": json.dumps(payload)}],
                "isError": False,
            },
        },
    )

def _mcp_error(text: str) -> Response:
    return Response(
        200,
        json={
            "jsonrpc": "2.0",
            "id": "1",
            "result": {
                "content": [{"type": "text", "text": text}],
                "isError": True,
            },
        },
    )

_PORTFOLIO_PAYLOAD = {
    "portfolio": {"value": 10000, "peak": 10500, "cash": 9000},
    "positions": [],
}

_FILL_PAYLOAD = {
    "status": "fill",
    "fill": {"id": "trade-abc", "price": "3000", "value": "900"},
}

_REJECT_PAYLOAD = {
    "status": "reject",
    "reject": {"reason": "missing_stop_loss", "tag": "missing_stop_loss"},
}

_LONG_INTENT = TradeIntent(
    asset="ETH",
    action="LONG",
    size_pct=Decimal("0.1"),
    stop_loss=Decimal("2700"),
    take_profit=Decimal("3500"),
    strategy="trend_following",
    reasoning="EMA crossover",
    trigger_reason="clock",
    confidence=0.85,
)

_FLAT_INTENT = TradeIntent(
    asset="ETH",
    action="FLAT",
    size_pct=Decimal("1.0"),
    stop_loss=None,
    take_profit=None,
    strategy="trend_following",
    reasoning="Take profit",
    trigger_reason="clock",
    confidence=0.9,
)


def _make_client() -> GoTradingMCPClient:
    return GoTradingMCPClient(base_url=_BASE, agent_id=_AGENT_ID)


# ── get_portfolio ─────────────────────────────────────────────────────────────

@respx.mock
async def test_get_portfolio_rpc_shape():
    """get_portfolio sends tools/call to /mcp/trading with X-Agent-ID header."""
    route = respx.post(_MCP_URL).mock(return_value=_mcp_ok(_PORTFOLIO_PAYLOAD))

    client = _make_client()
    snapshot = await client.get_portfolio()

    assert route.called
    req = route.calls[0].request
    assert req.headers["X-Agent-ID"] == _AGENT_ID

    body = json.loads(req.content)
    assert body["method"] == "tools/call"
    assert body["params"]["name"] == "trade/get_portfolio"

    assert snapshot.total_usd == Decimal("10000")
    assert snapshot.stablecoin_balance == Decimal("9000")
    assert snapshot.open_position_count == 0


@respx.mock
async def test_get_portfolio_drawdown_computed():
    """Drawdown is computed from peak - value."""
    respx.post(_MCP_URL).mock(return_value=_mcp_ok(_PORTFOLIO_PAYLOAD))
    client = _make_client()
    snapshot = await client.get_portfolio()
    # (10500 - 10000) / 10500 * 100 ≈ 4.76%
    assert snapshot.current_drawdown_pct > Decimal("4")
    assert snapshot.current_drawdown_pct < Decimal("5")


@respx.mock
async def test_get_portfolio_maps_positions():
    """Open positions are converted to OpenPositionView with fractional size_pct."""
    payload = {
        "portfolio": {"value": 10000, "peak": 10000, "cash": 0},
        "positions": [
            {
                "pair": "ETH-USD",
                "avg_price": "2900",
                "stop_loss": "2700",
                "take_profit": "3500",
                "concentration_pct": "15.5",
                "strategy": "trend",
            }
        ],
    }
    respx.post(_MCP_URL).mock(return_value=_mcp_ok(payload))
    client = _make_client()
    snapshot = await client.get_portfolio()

    assert len(snapshot.open_positions) == 1
    pos = snapshot.open_positions[0]
    assert pos.asset == "ETH"
    assert pos.entry_price == Decimal("2900")
    assert pos.stop_loss == Decimal("2700")
    # 15.5% -> 0.155 fraction
    assert abs(pos.size_pct - Decimal("0.155")) < Decimal("0.001")


# ── execute_swap LONG ─────────────────────────────────────────────────────────

@respx.mock
async def test_execute_long_sends_buy_order():
    """LONG intent sends buy order with computed value and SL/TP params."""
    # First call: get_portfolio; second call: submit_order
    respx.post(_MCP_URL).mock(
        side_effect=[_mcp_ok(_PORTFOLIO_PAYLOAD), _mcp_ok(_FILL_PAYLOAD)]
    )

    client = _make_client()
    result = await client.execute_swap(_LONG_INTENT)

    assert result["status"] == "executed"
    assert result["tx_hash"] == "trade-abc"

    submit_call = respx.calls[1]
    body = json.loads(submit_call.request.content)
    args = body["params"]["arguments"]
    assert body["params"]["name"] == "trade/submit_order"
    assert args["pair"] == "ETH-USD"
    assert args["side"] == "buy"
    # value = cash(9000) * size_pct(0.1) = 900
    assert float(args["value"]) == pytest.approx(900.0, rel=1e-3)
    assert args["params"]["stop_loss"] == pytest.approx(2700.0)
    assert args["params"]["take_profit"] == pytest.approx(3500.0)
    assert args["params"]["confidence"] == pytest.approx(0.85)


@respx.mock
async def test_execute_long_rejected_maps_to_rejected_status():
    """A Swiftward rejection is surfaced as status=rejected with reason."""
    respx.post(_MCP_URL).mock(
        side_effect=[_mcp_ok(_PORTFOLIO_PAYLOAD), _mcp_ok(_REJECT_PAYLOAD)]
    )

    client = _make_client()
    result = await client.execute_swap(_LONG_INTENT)

    assert result["status"] == "rejected"
    assert "missing_stop_loss" in result["reason"]


# ── execute_swap FLAT ─────────────────────────────────────────────────────────

@respx.mock
async def test_execute_flat_sends_sell_order():
    """FLAT intent sends sell order with sentinel value (clamped server-side)."""
    respx.post(_MCP_URL).mock(return_value=_mcp_ok(_FILL_PAYLOAD))

    client = _make_client()
    await client.execute_swap(_FLAT_INTENT)

    body = json.loads(respx.calls[0].request.content)
    args = body["params"]["arguments"]
    assert body["params"]["name"] == "trade/submit_order"
    assert args["pair"] == "ETH-USD"
    assert args["side"] == "sell"
    assert args["value"] == 999_999_999  # sentinel - Go clamps to position


# ── unsupported action ────────────────────────────────────────────────────────

async def test_execute_swap_unsupported_action_raises(monkeypatch):
    """An action not handled by the adapter raises ValueError without a network call."""
    # Patch action after construction to bypass pydantic validation.
    client = _make_client()
    intent = _FLAT_INTENT.model_copy(update={"action": "HOLD"})
    with pytest.raises(ValueError, match="Unsupported action"):
        await client.execute_swap(intent)


# ── health check ──────────────────────────────────────────────────────────────

@respx.mock
async def test_health_check_true_on_200():
    respx.get(f"{_BASE}/health").mock(return_value=Response(200, json={"status": "ok"}))
    assert await _make_client().health_check() is True


@respx.mock
async def test_health_check_false_on_503():
    respx.get(f"{_BASE}/health").mock(return_value=Response(503))
    assert await _make_client().health_check() is False


async def test_health_check_false_on_connect_error():
    import httpx
    with respx.mock:
        respx.get(f"{_BASE}/health").mock(side_effect=httpx.ConnectError("refused"))
        assert await _make_client().health_check() is False


# ── error handling ────────────────────────────────────────────────────────────

@respx.mock
async def test_http_500_raises_mcp_error():
    respx.post(_MCP_URL).mock(return_value=Response(500, text="Internal Server Error"))
    client = _make_client()
    with pytest.raises(MCPError):
        await client.get_portfolio()


@respx.mock
async def test_jsonrpc_error_raises_mcp_error():
    respx.post(_MCP_URL).mock(
        return_value=Response(
            200,
            json={"jsonrpc": "2.0", "id": "1", "error": {"code": -32600, "message": "bad"}},
        )
    )
    client = _make_client()
    with pytest.raises(MCPError):
        await client.get_portfolio()


@respx.mock
async def test_mcp_is_error_raises_mcp_error():
    """isError=true in MCP result should raise MCPError."""
    respx.post(_MCP_URL).mock(return_value=_mcp_error("tool execution failed"))
    client = _make_client()
    with pytest.raises(MCPError, match="tool error"):
        await client.get_portfolio()


# ── SSE response path ─────────────────────────────────────────────────────────

@respx.mock
async def test_sse_response_parsed():
    """Go server may respond with text/event-stream; client handles it."""
    sse_body = (
        "data: " + json.dumps({
            "jsonrpc": "2.0",
            "id": "1",
            "result": {
                "content": [{"type": "text", "text": json.dumps(_PORTFOLIO_PAYLOAD)}],
                "isError": False,
            },
        }) + "\n\n"
    )
    respx.post(_MCP_URL).mock(
        return_value=Response(200, text=sse_body, headers={"content-type": "text/event-stream"})
    )
    client = _make_client()
    snapshot = await client.get_portfolio()
    assert snapshot.total_usd == Decimal("10000")
