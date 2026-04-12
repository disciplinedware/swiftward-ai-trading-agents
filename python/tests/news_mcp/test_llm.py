"""Tests for NewsLLMScorer — prompt construction, JSON parsing, score clamping."""
import pytest

from news_mcp.service.llm import _build_prompt, _parse_response

_ASSETS = ["BTC", "ETH", "SOL"]

_TS = "2026-03-20T10:00:00Z"

_HEADLINES = [
    {"title": f"BTC news {i}", "description": "", "published_at": _TS, "url": "", "source": ""}
    for i in range(15)
] + [
    {"title": "ETH upgrade delayed", "description": "", "published_at": _TS, "url": "", "source": ""},  # noqa: E501
]


# ---------------------------------------------------------------------------
# _build_prompt
# ---------------------------------------------------------------------------

def test_build_prompt_caps_headlines_at_50():
    many = [
        {"title": f"headline {i}", "description": "", "published_at": _TS, "url": "", "source": ""}
        for i in range(60)
    ]
    prompt = _build_prompt(["BTC"], many)
    assert prompt.count("headline") == 50


def test_build_prompt_no_headlines_shows_placeholder():
    prompt = _build_prompt(["SOL"], [])
    assert "(no recent news)" in prompt


def test_build_prompt_lists_asset_names_header():
    prompt = _build_prompt(["BTC", "ETH"], _HEADLINES)
    assert "Assets to score: BTC, ETH" in prompt


def test_build_prompt_flat_list_format():
    prompt = _build_prompt(["BTC"], _HEADLINES[:2])
    # Should NOT have per-asset section headers
    assert "\nBTC:" not in prompt
    # Should have flat bullet entries
    assert "- " in prompt


# ---------------------------------------------------------------------------
# _parse_response
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,content,expected_sentiment,expected_macro,expected_reason", [
    (
        "valid response with macro triggered",
        '{"sentiment": {"BTC": 0.7, "ETH": -0.3},'
        ' "macro": {"triggered": true, "reason": "ETF approved"}}',
        {"BTC": 0.7, "ETH": -0.3},
        True,
        "ETF approved",
    ),
    (
        "macro not triggered",
        '{"sentiment": {"BTC": 0.1, "ETH": 0.0}, "macro": {"triggered": false, "reason": null}}',
        {"BTC": 0.1, "ETH": 0.0},
        False,
        None,
    ),
    (
        "missing asset defaults to 0.0",
        '{"sentiment": {"BTC": 0.5}, "macro": {"triggered": false, "reason": null}}',
        {"BTC": 0.5, "ETH": 0.0},
        False,
        None,
    ),
])
def test_parse_response_valid(name, content, expected_sentiment, expected_macro, expected_reason):
    assets = list(expected_sentiment.keys())
    result = _parse_response(content, assets)
    assert result.sentiment == expected_sentiment, name
    assert result.macro_flag == expected_macro, name
    assert result.macro_reason == expected_reason, name


@pytest.mark.parametrize("name,content", [
    ("invalid JSON", "not json at all"),
    ("empty string", ""),
    ("truncated JSON", '{"sentiment": {'),
])
def test_parse_response_invalid_json_raises(name, content):
    with pytest.raises(ValueError):
        _parse_response(content, ["BTC", "ETH"])


@pytest.mark.parametrize("name,score,expected_clamped", [
    ("above max", 1.5, 1.0),
    ("below min", -2.0, -1.0),
    ("at max", 1.0, 1.0),
    ("at min", -1.0, -1.0),
    ("in range", 0.3, 0.3),
])
def test_parse_response_clamps_scores(name, score, expected_clamped):
    content = f'{{"sentiment": {{"BTC": {score}}}, "macro": {{"triggered": false, "reason": null}}}}'  # noqa: E501
    result = _parse_response(content, ["BTC"])
    assert result.sentiment["BTC"] == expected_clamped, name
