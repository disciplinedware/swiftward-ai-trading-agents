from unittest.mock import AsyncMock, patch

import pytest

from common.cache import RedisCache


@pytest.fixture
def cache():
    return RedisCache("redis://localhost:6379/0")


async def test_cache_hit_returns_stored_value(cache):
    mock_client = AsyncMock()
    mock_client.ping = AsyncMock()
    mock_client.get = AsyncMock(return_value='{"price": "67000.0"}')
    cache._client = mock_client

    result = await cache.get("price_feed:BTCUSDT:ticker")
    assert result == {"price": "67000.0"}
    mock_client.get.assert_awaited_once_with("price_feed:BTCUSDT:ticker")


async def test_cache_miss_returns_none(cache):
    mock_client = AsyncMock()
    mock_client.get = AsyncMock(return_value=None)
    cache._client = mock_client

    result = await cache.get("price_feed:BTCUSDT:ticker")
    assert result is None


async def test_cache_set_uses_setex_with_ttl(cache):
    mock_client = AsyncMock()
    cache._client = mock_client

    await cache.set("price_feed:BTCUSDT:ticker", {"price": "67000.0"}, ttl=30)
    mock_client.setex.assert_awaited_once()
    args = mock_client.setex.call_args
    assert args[0][0] == "price_feed:BTCUSDT:ticker"
    assert args[0][1] == 30


async def test_redis_unavailable_get_returns_none(cache):
    """When Redis connect fails, all gets return None (fallthrough)."""
    with patch("redis.asyncio.from_url") as mock_from_url:
        mock_client = AsyncMock()
        mock_client.ping = AsyncMock(side_effect=ConnectionError("refused"))
        mock_from_url.return_value = mock_client
        await cache.connect()

    assert cache._client is None
    result = await cache.get("any_key")
    assert result is None


async def test_redis_unavailable_set_is_noop(cache):
    """When Redis is unavailable, set is silently skipped."""
    cache._client = None
    # Should not raise
    await cache.set("any_key", {"x": 1}, ttl=30)


async def test_redis_get_exception_returns_none(cache):
    """If Redis throws during get, return None rather than propagating."""
    mock_client = AsyncMock()
    mock_client.get = AsyncMock(side_effect=Exception("redis blew up"))
    cache._client = mock_client

    result = await cache.get("any_key")
    assert result is None
