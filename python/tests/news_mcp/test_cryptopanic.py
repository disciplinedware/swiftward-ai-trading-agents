"""Tests for CryptoPanicClient — response parsing, error handling."""
import httpx
import pytest
import respx

from common.exceptions import MCPError
from news_mcp.infra.cryptopanic import CryptoPanicClient, _parse_post

_TOKEN = "testtoken"
_BASE = "https://cryptopanic.com/api/developer/v2"


# ---------------------------------------------------------------------------
# _parse_post — pure helper
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,raw,expected", [
    (
        "full post",
        {
            "title": "Bitcoin hits new high",
            "description": "BTC surges past $70k on ETF inflows.",
            "url": "https://example.com/btc",
            "published_at": "2026-03-20T10:00:00Z",
            "source": {"title": "CoinDesk"},
        },
        {
            "title": "Bitcoin hits new high",
            "description": "BTC surges past $70k on ETF inflows.",
            "url": "https://example.com/btc",
            "published_at": "2026-03-20T10:00:00Z",
            "source": "CoinDesk",
        },
    ),
    (
        "null description normalised to empty string",
        {
            "title": "Crypto market rallies",
            "description": None,
            "url": "https://example.com/crypto",
            "published_at": "2026-03-20T09:00:00Z",
            "source": {"title": "Decrypt"},
        },
        {
            "title": "Crypto market rallies",
            "description": "",
            "url": "https://example.com/crypto",
            "published_at": "2026-03-20T09:00:00Z",
            "source": "Decrypt",
        },
    ),
    (
        "missing optional fields default to empty",
        {},
        {"title": "", "description": "", "url": "", "published_at": "", "source": ""},
    ),
])
def test_parse_post(name, raw, expected):
    assert _parse_post(raw) == expected, name


# ---------------------------------------------------------------------------
# CryptoPanicClient.get_posts — HTTP layer
# ---------------------------------------------------------------------------

@pytest.fixture
def client():
    return CryptoPanicClient(auth_token=_TOKEN)


@respx.mock
async def test_get_posts_returns_parsed_posts(client):
    respx.get(f"{_BASE}/posts/").mock(return_value=httpx.Response(200, json={
        "results": [
            {
                "title": "BTC soars",
                "url": "https://example.com/1",
                "published_at": "2026-03-20T10:00:00Z",
                "source": {"title": "CoinDesk"},
            },
            {
                "title": "ETH upgrade",
                "url": "https://example.com/2",
                "published_at": "2026-03-20T09:00:00Z",
                "source": {"title": "Decrypt"},
            },
        ]
    }))

    await client.connect()
    posts = await client.get_posts(["BTC", "ETH"])
    await client.close()

    assert len(posts) == 2
    assert posts[0]["title"] == "BTC soars"
    assert posts[1]["title"] == "ETH upgrade"


@respx.mock
async def test_get_posts_respects_limit(client):
    results = [
        {"title": f"Post {i}", "url": "", "published_at": "", "source": {}}
        for i in range(10)
    ]
    respx.get(f"{_BASE}/posts/").mock(return_value=httpx.Response(200, json={"results": results}))

    await client.connect()
    posts = await client.get_posts(["BTC"], limit=3)
    await client.close()

    assert len(posts) == 3


@respx.mock
async def test_get_posts_non_200_raises_mcp_error(client):
    respx.get(f"{_BASE}/posts/").mock(return_value=httpx.Response(429, text="rate limited"))

    await client.connect()
    with pytest.raises(MCPError, match="429"):
        await client.get_posts(["BTC"])
    await client.close()


@respx.mock
async def test_get_posts_network_error_raises_mcp_error(client):
    respx.get(f"{_BASE}/posts/").mock(side_effect=httpx.ConnectError("timeout"))

    await client.connect()
    with pytest.raises(MCPError, match="request failed"):
        await client.get_posts(["BTC"])
    await client.close()
