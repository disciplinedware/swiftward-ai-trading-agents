from datetime import UTC, datetime

from common.cache import RedisCache
from common.log import get_logger
from fear_greed_mcp.infra.alternative_me import AlternativeMeClient

logger = get_logger(__name__)

_CACHE_KEY = "fear_greed:current"
_CACHE_TTL = 60 * 60 * 25  # 25 hours — safety net; UTC-date check is the real gate


class FearGreedService:
    def __init__(self, client: AlternativeMeClient, cache: RedisCache) -> None:
        self._client = client
        self._cache = cache

    async def get_index(self) -> dict:
        """Return current Fear & Greed index. Cached per UTC calendar day."""
        today = _utc_date_str()

        cached = await self._cache.get(_CACHE_KEY)
        if cached is not None and cached.get("date") == today:
            return cached["data"]

        # Cache miss or stale (different UTC date) — fetch fresh.
        raw = await self._client.get_index()
        entry = _parse_entry_current(raw)
        await self._cache.set(_CACHE_KEY, {"date": today, "data": entry}, _CACHE_TTL)
        return entry

    async def get_historical(self, limit: int) -> list[dict]:
        """Return last `limit` daily Fear & Greed values. Always fetched fresh."""
        raw_list = await self._client.get_historical(limit)
        return [_parse_entry_historical(r) for r in raw_list]


# ---------------------------------------------------------------------------
# Pure helpers
# ---------------------------------------------------------------------------


def _utc_date_str() -> str:
    return datetime.now(UTC).date().isoformat()


def _parse_entry_current(raw: dict) -> dict:
    """Convert an Alternative.me data point to LLM-friendly format for get_index."""
    return {
        "value": int(raw["value"]),
        "classification": raw["value_classification"],
        "updated_at": _unix_to_iso(raw["timestamp"]),
    }


def _parse_entry_historical(raw: dict) -> dict:
    """Convert an Alternative.me data point to LLM-friendly format for get_historical."""
    return {
        "value": int(raw["value"]),
        "classification": raw["value_classification"],
        "timestamp": _unix_to_iso(raw["timestamp"]),
    }


def _unix_to_iso(ts: str | int) -> str:
    return datetime.fromtimestamp(int(ts), tz=UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
