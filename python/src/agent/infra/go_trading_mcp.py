"""GoTradingMCPClient - adapter from Python TradeIntent to Go Trading MCP.

Replaces TradingMCPClient when trading_backend="go" in config. Talks to the Go
trading-server at /mcp/trading instead of the Python trading_mcp at /mcp.

Key differences from TradingMCPClient:
  - Sends X-Agent-ID header (required by Go Trading MCP)
  - Uses Go tool names: trade/get_portfolio, trade/submit_order
  - Converts TradeIntent (asset, action, size_pct) to Go format (pair, side, value)
  - Reshapes Go portfolio response to PortfolioSnapshot
"""

import json
import uuid
from decimal import Decimal

import httpx

from common.exceptions import MCPError
from common.log import get_logger
from common.models.portfolio_snapshot import OpenPositionView, PortfolioSnapshot
from common.models.trade_intent import TradeIntent

logger = get_logger(__name__)


class GoTradingMCPClient:
    """MCP client that talks to the Go Trading MCP server."""

    def __init__(
        self,
        base_url: str,
        agent_id: str,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._agent_id = agent_id
        self._client = client or httpx.AsyncClient(timeout=60.0)

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health")
            return resp.status_code == 200
        except Exception:
            return False

    async def get_portfolio(self) -> PortfolioSnapshot:
        body = await self._call("trade/get_portfolio", {})
        data = body["result"]

        portfolio = data.get("portfolio", {})
        positions_raw = data.get("positions", [])

        # Compute drawdown from peak
        value = Decimal(str(portfolio.get("value", "0")))
        peak = Decimal(str(portfolio.get("peak", "0")))
        drawdown_pct = Decimal("0")
        if peak > 0:
            drawdown_pct = max(Decimal("0"), (peak - value) / peak * 100)

        open_positions = []
        for pos in positions_raw:
            pair = pos.get("pair", "")
            asset = pair.rsplit("-", 1)[0] if "-" in pair else pair

            # Go returns concentration_pct (percentage, e.g. "15.23").
            # Python OpenPositionView.size_pct expects a fraction (0.1523).
            conc_pct = Decimal(str(pos.get("concentration_pct", "0")))
            size_frac = conc_pct / 100 if conc_pct > 0 else Decimal("0")

            open_positions.append(
                OpenPositionView(
                    asset=asset,
                    entry_price=Decimal(str(pos.get("avg_price", "0"))),
                    stop_loss=Decimal(str(pos.get("stop_loss", "0"))),
                    take_profit=Decimal(str(pos.get("take_profit", "0"))),
                    size_pct=size_frac,
                    strategy=pos.get("strategy", "unknown"),
                )
            )

        return PortfolioSnapshot(
            total_usd=value,
            stablecoin_balance=Decimal(str(portfolio.get("cash", "0"))),
            open_position_count=len(positions_raw),
            realized_pnl_today=Decimal("0"),
            current_drawdown_pct=drawdown_pct,
            open_positions=open_positions,
        )

    async def execute_swap(self, intent: TradeIntent) -> dict:
        if intent.action == "LONG":
            return await self._execute_long(intent)
        if intent.action == "FLAT":
            return await self._execute_flat(intent)
        raise ValueError(f"Unsupported action: {intent.action}")

    async def _execute_long(self, intent: TradeIntent) -> dict:
        # Fetch portfolio to compute value from size_pct
        portfolio = await self.get_portfolio()
        cash = portfolio.stablecoin_balance
        value = cash * intent.size_pct

        params: dict = {}
        if intent.stop_loss is not None and intent.stop_loss > 0:
            params["stop_loss"] = float(intent.stop_loss)
        if intent.take_profit is not None and intent.take_profit > 0:
            params["take_profit"] = float(intent.take_profit)
        params["strategy"] = intent.strategy
        params["reasoning"] = intent.reasoning
        params["trigger_reason"] = intent.trigger_reason
        params["confidence"] = intent.confidence

        result = await self._call(
            "trade/submit_order",
            {
                "pair": f"{intent.asset}-USD",
                "side": "buy",
                "value": str(value),  # string preserves decimal precision; Go accepts both
                "params": params,
            },
        )
        return self._map_response(result["result"])

    async def _execute_flat(self, intent: TradeIntent) -> dict:
        params: dict = {
            "strategy": intent.strategy,
            "reasoning": intent.reasoning,
            "trigger_reason": intent.trigger_reason,
            "confidence": intent.confidence,
        }

        result = await self._call(
            "trade/submit_order",
            {
                "pair": f"{intent.asset}-USD",
                "side": "sell",
                "value": 999_999_999,  # Go clamps to actual position size
                "params": params,
            },
        )
        return self._map_response(result["result"])

    def _map_response(self, data: dict) -> dict:
        """Map Go Trading MCP response to Python ExecutionResult shape."""
        status = data.get("status", "")
        if status == "fill":
            fill = data.get("fill", {})
            return {
                "status": "executed",
                "tx_hash": fill.get("tx_hash", fill.get("id", "")),
                "executed_price": fill.get("price", "0"),
                "slippage_pct": "0",
                "size_usd": fill.get("value", "0"),
                "reason": "",
            }
        if status == "reject":
            reject = data.get("reject", {})
            return {
                "status": "rejected",
                "tx_hash": "",
                "executed_price": "0",
                "slippage_pct": "0",
                "size_usd": "0",
                "reason": reject.get("reason", "unknown"),
            }
        return {"status": status, "tx_hash": "", "reason": "unexpected response"}

    async def end_cycle(self, summary: str) -> None:
        """Post session checkpoint via trade/end_cycle."""
        await self._call("trade/end_cycle", {"summary": summary})

    async def _call(self, tool_name: str, arguments: dict) -> dict:
        """Call Go Trading MCP tool over JSON-RPC 2.0."""
        logger.debug(
            "go_trading_mcp call",
            tool=tool_name,
            arguments=arguments,
            base_url=self._base_url,
        )
        payload = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "tools/call",
            "params": {"name": tool_name, "arguments": arguments},
        }
        try:
            response = await self._client.post(
                f"{self._base_url}/mcp/trading",
                json=payload,
                headers={
                    "Accept": "application/json, text/event-stream",
                    "X-Agent-ID": self._agent_id,
                },
            )
        except httpx.HTTPError as exc:
            raise MCPError(f"{tool_name} request failed: {exc}") from exc

        if response.status_code != 200:
            raise MCPError(f"{tool_name} returned HTTP {response.status_code}")

        if "text/event-stream" in response.headers.get("content-type", ""):
            body = None
            for line in response.text.splitlines():
                if line.startswith("data: "):
                    data_str = line[6:]
                    if not data_str.strip():
                        continue
                    body = json.loads(data_str)
                    break
            if body is None:
                raise MCPError(f"{tool_name}: empty SSE response")
        else:
            body = response.json()

        if "error" in body:
            raise MCPError(f"{tool_name} JSON-RPC error: {body['error']}")

        mcp_result = body["result"]
        if isinstance(mcp_result, dict) and "content" in mcp_result:
            content = mcp_result["content"]
            if content and content[0].get("type") == "text":
                text = content[0]["text"]
                if mcp_result.get("isError"):
                    raise MCPError(f"{tool_name} tool error: {text}")
                if not text:
                    raise MCPError(f"{tool_name}: empty text content")
                result = {"result": json.loads(text)}
                logger.debug("go_trading_mcp result", tool=tool_name, result=result)
                return result

        logger.debug("go_trading_mcp result", tool=tool_name, result=mcp_result)
        return {"result": mcp_result}
