"""Shared JSON-RPC / SSE transport mixin for FastMCP streamable-http servers."""

import json
import uuid

import httpx

from common.exceptions import MCPError
from common.log import get_logger

logger = get_logger(__name__)


class MCPClientMixin:
    """Mixin providing _call() and health_check() for FastMCP HTTP clients.

    Concrete classes must set self._base_url and self._client before calling these methods.
    """

    _base_url: str
    _client: httpx.AsyncClient

    async def health_check(self) -> bool:
        try:
            resp = await self._client.get(f"{self._base_url}/health")
            return resp.status_code == 200
        except Exception:
            return False

    async def _call(self, tool_name: str, arguments: dict) -> dict:
        """Call a FastMCP tool over streamable-http (JSON-RPC 2.0 + SSE)."""
        logger.debug(
            f"mcp call {tool_name}",
            tool=tool_name, arguments=arguments, base_url=self._base_url,
        )
        payload = {
            "jsonrpc": "2.0",
            "id": str(uuid.uuid4()),
            "method": "tools/call",
            "params": {"name": tool_name, "arguments": arguments},
        }
        try:
            response = await self._client.post(
                f"{self._base_url}/mcp",
                json=payload,
                headers={"Accept": "application/json, text/event-stream"},
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
                        continue  # skip empty data lines (SSE heartbeats)
                    body = json.loads(data_str)
                    break
            if body is None:
                raise MCPError(f"{tool_name}: empty SSE response (raw={response.text!r})")
        else:
            body = response.json()

        if "error" in body:
            raise MCPError(f"{tool_name} JSON-RPC error: {body['error']}")

        mcp_result = body["result"]
        if isinstance(mcp_result, dict) and "content" in mcp_result:
            content = mcp_result["content"]
            if content and content[0].get("type") == "text":
                text = content[0]["text"]
                # FastMCP sets isError=true when the tool raised an exception;
                # in that case text is a plain error string, not JSON.
                if mcp_result.get("isError"):
                    raise MCPError(f"{tool_name} tool error: {text}")
                if not text:
                    raise MCPError(f"{tool_name}: MCP returned empty text content")

                result = {"result": json.loads(text)}

                logger.debug(f"mcp result {tool_name}", tool=tool_name, result=result)

                return result

        logger.debug(f"mcp result {tool_name}", tool=tool_name, result=mcp_result)

        return {"result": mcp_result}
