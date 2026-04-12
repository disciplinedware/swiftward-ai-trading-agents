"""Tests for CoinDeskClient — response parsing, error handling."""
import httpx
import pytest
import respx

from common.exceptions import MCPError
from news_mcp.infra.coindesk import CoinDeskClient, _parse_article

_KEY = "testkey"
_BASE = "https://data-api.coindesk.com"


# ---------------------------------------------------------------------------
# _parse_article — pure helper
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,raw,expected", [
    (
        "full article",
        {
            "TITLE": "Bitcoin ETF sees record inflows",
            "BODY": "Institutional demand surges.",
            "URL": "https://coindesk.com/article/1",
            "PUBLISHED_ON": 1774915200,  # 2026-03-31T00:00:00+00:00
            "SOURCE_DATA": {"NAME": "CoinDesk"},
        },
        {
            "title": "Bitcoin ETF sees record inflows",
            "description": "Institutional demand surges.",
            "url": "https://coindesk.com/article/1",
            "published_at": "2026-03-31T00:00:00+00:00",
            "source": "CoinDesk",
        },
    ),
    (
        "missing optional fields default to empty",
        {},
        {"title": "", "description": "", "url": "", "published_at": "", "source": ""},
    ),
    (
        "null body normalised to empty string",
        {"TITLE": "News", "BODY": None, "SOURCE_DATA": {}},
        {"title": "News", "description": "", "url": "", "published_at": "", "source": ""},
    ),
])
def test_parse_article(name, raw, expected):
    assert _parse_article(raw) == expected, name


# ---------------------------------------------------------------------------
# CoinDeskClient.get_posts — HTTP layer
# ---------------------------------------------------------------------------

@pytest.fixture
def client():
    return CoinDeskClient(api_key=_KEY)


@respx.mock
async def test_get_posts_returns_parsed_articles(client):
    respx.get(f"{_BASE}/news/v1/article/list").mock(return_value=httpx.Response(200, json={
        "Data": [
            {
                "TITLE": "BTC rallies",
                "BODY": "Bitcoin up 5%.",
                "URL": "https://coindesk.com/1",
                "PUBLISHED_ON": 1743379200,
                "SOURCE_DATA": {"NAME": "CoinDesk"},
            },
            {
                "TITLE": "ETH upgrade",
                "BODY": "",
                "URL": "https://coindesk.com/2",
                "PUBLISHED_ON": 1743375600,
                "SOURCE_DATA": {"NAME": "CoinDesk"},
            },
        ]
    }))

    await client.connect()
    posts = await client.get_posts(["BTC", "ETH"])
    await client.close()

    assert len(posts) == 2
    assert posts[0]["title"] == "BTC rallies"
    assert posts[0]["source"] == "CoinDesk"


@respx.mock
async def test_get_posts_respects_limit(client):
    respx.get(f"{_BASE}/news/v1/article/list").mock(return_value=httpx.Response(200, json={
        "Data": [
            {"TITLE": f"Article {i}", "BODY": "", "URL": "", "PUBLISHED_ON": 0, "SOURCE_DATA": {}}
            for i in range(5)
        ]
    }))

    await client.connect()
    posts = await client.get_posts([], limit=5)
    await client.close()

    assert len(posts) == 5


@respx.mock
async def test_get_posts_non_200_raises_mcp_error(client):
    respx.get(f"{_BASE}/news/v1/article/list").mock(
        return_value=httpx.Response(401, text="unauthorized")
    )

    await client.connect()
    with pytest.raises(MCPError, match="401"):
        await client.get_posts([])
    await client.close()


@respx.mock
async def test_get_posts_network_error_raises_mcp_error(client):
    respx.get(f"{_BASE}/news/v1/article/list").mock(side_effect=httpx.ConnectError("timeout"))

    await client.connect()
    with pytest.raises(MCPError, match="request failed"):
        await client.get_posts([])
    await client.close()
