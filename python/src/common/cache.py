import json

import redis.asyncio as aioredis

from common.log import get_logger

logger = get_logger(__name__)


class RedisCache:
    """Async Redis cache wrapper. Falls through (returns None) if Redis is unavailable."""

    def __init__(self, redis_url: str) -> None:
        self._url = redis_url
        self._client: aioredis.Redis | None = None

    async def connect(self) -> None:
        try:
            self._client = aioredis.from_url(self._url, decode_responses=True)
            await self._client.ping()  # type: ignore[misc]
        except Exception as e:
            logger.warning("Redis unavailable, caching disabled: %s", e)
            self._client = None

    async def get(self, key: str) -> list | dict | None:
        if self._client is None:
            return None
        try:
            value = await self._client.get(key)
            return json.loads(value) if value is not None else None
        except Exception as e:
            logger.warning("Redis get failed for key %s: %s", key, e)
            return None

    async def set(self, key: str, value: list | dict, ttl: int) -> None:
        if self._client is None:
            return
        try:
            await self._client.setex(key, ttl, json.dumps(value))
        except Exception as e:
            logger.warning("Redis set failed for key %s: %s", key, e)

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None
