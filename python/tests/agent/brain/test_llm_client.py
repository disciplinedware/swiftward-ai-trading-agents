"""Tests for agent.brain.llm_client."""
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from agent.brain.llm_client import LLMClient, _normalize_encoding, _parse_decision
from common.exceptions import MCPError

# ---------------------------------------------------------------------------
# Encoding normalization
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("input_text,expected", [
    # Chinese left/right quotes
    ('\u201cverdict\u201d', '"verdict"'),
    # Fullwidth brackets
    ('\uff3blist\uff3d', '[list]'),
    # Fullwidth braces
    ('\uff5bobj\uff5d', '{obj}'),
    # Fullwidth colon and comma
    ('key\uff1avalue\uff0cnext', 'key:value,next'),
    # No-op on ASCII
    ('{"verdict": "RISK_ON"}', '{"verdict": "RISK_ON"}'),
    # Mixed
    ('\u201cverdict\u201d\uff1a\u201cRISK_ON\u201d', '"verdict":"RISK_ON"'),
])
def test_normalize_encoding(input_text, expected):
    assert _normalize_encoding(input_text) == expected


# ---------------------------------------------------------------------------
# JSON parsing — 4-level fallback
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,response,expected_key,expected_val", [
    (
        "decision tag with code fence",
        '<reasoning>analysis</reasoning>\n<decision>\n```json\n{"verdict":"RISK_ON"}\n```\n</decision>',
        "verdict", "RISK_ON",
    ),
    (
        "decision tag without code fence",
        '<decision>{"verdict":"UNCERTAIN"}</decision>',
        "verdict", "UNCERTAIN",
    ),
    (
        "array fallback from [",
        '[{"asset":"SOL","regime":"BREAKOUT"}]',
        None, None,  # list returned, check separately
    ),
    (
        "object fallback from {",
        'some text before {"verdict":"RISK_OFF"} more text',
        "verdict", "RISK_OFF",
    ),
])
def test_parse_decision_fallback(name, response, expected_key, expected_val):
    result = _parse_decision(response)
    if expected_key is not None:
        assert result[expected_key] == expected_val, f"{name}: {result}"
    else:
        assert isinstance(result, list), f"{name}: expected list"


def test_parse_decision_array_from_bracket():
    response = '[{"asset":"SOL","regime":"BREAKOUT"},{"asset":"AVAX","regime":"RANGING"}]'
    result = _parse_decision(response)
    assert isinstance(result, list)
    assert result[0]["asset"] == "SOL"


def test_parse_decision_raises_on_no_json():
    with pytest.raises(MCPError, match="no parseable JSON"):
        _parse_decision("This is just plain text with no JSON at all.")


def test_parse_decision_chinese_quotes_normalized():
    # Fullwidth-quoted JSON object — use level 3 fallback
    response = '{\u201cverdict\u201d:\u201cRISK_ON\u201d}'
    result = _parse_decision(response)
    assert result["verdict"] == "RISK_ON"


# ---------------------------------------------------------------------------
# LLMClient.call() — success and retry behaviour (mocked AsyncOpenAI)
# ---------------------------------------------------------------------------

def _make_client(retries: int = 3) -> LLMClient:
    return LLMClient(
        base_url="http://localhost:11434/v1",
        model="llama3.2",
        api_key="test",
        max_tokens=100,
        retries=retries,
    )


def _mock_response(content: str):
    choice = MagicMock()
    choice.message.content = content
    resp = MagicMock()
    resp.choices = [choice]
    return resp


async def test_call_success():
    client = _make_client()
    xml = (
        "<reasoning>Market looks bullish</reasoning>\n"
        '<decision>\n```json\n{"verdict":"RISK_ON","reason":"strong trend"}\n```\n</decision>'
    )
    create_mock = AsyncMock(return_value=_mock_response(xml))
    with patch.object(client._completions, "create", new=create_mock):
        reasoning, decision = await client.call("sys", "user")
    assert reasoning == "Market looks bullish"
    assert decision["verdict"] == "RISK_ON"


async def test_call_missing_reasoning_returns_empty():
    client = _make_client()
    xml = '<decision>{"verdict":"UNCERTAIN","reason":"mixed"}</decision>'
    create_mock = AsyncMock(return_value=_mock_response(xml))
    with patch.object(client._completions, "create", new=create_mock):
        reasoning, decision = await client.call("sys", "user")
    assert reasoning == ""
    assert decision["verdict"] == "UNCERTAIN"


async def test_call_retries_on_transient_error_then_succeeds():
    client = _make_client(retries=3)
    xml = '<decision>{"verdict":"RISK_ON","reason":"ok"}</decision>'
    ok_response = _mock_response(xml)
    call_mock = AsyncMock(side_effect=[Exception("timeout"), ok_response])
    with patch.object(client._completions, "create", new=call_mock):
        with patch("agent.brain.llm_client.asyncio.sleep", new=AsyncMock()):
            reasoning, decision = await client.call("sys", "user")
    assert decision["verdict"] == "RISK_ON"
    assert call_mock.call_count == 2


async def test_call_all_retries_exhausted_raises():
    client = _make_client(retries=2)
    call_mock = AsyncMock(side_effect=Exception("network error"))
    with patch.object(client._completions, "create", new=call_mock):
        with patch("agent.brain.llm_client.asyncio.sleep", new=AsyncMock()):
            with pytest.raises(Exception, match="network error"):
                await client.call("sys", "user")
    assert call_mock.call_count == 2


async def test_call_json_parse_error_not_retried():
    """MCPError from JSON parsing is not retried."""
    client = _make_client(retries=3)
    call_mock = AsyncMock(return_value=_mock_response("no json here at all"))
    with patch.object(client._completions, "create", new=call_mock):
        with pytest.raises(MCPError):
            await client.call("sys", "user")
    assert call_mock.call_count == 1
