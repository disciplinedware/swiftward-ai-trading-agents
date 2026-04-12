"""Tests for FearGreedService — cache hit/miss, midnight invalidation, degradation."""
from unittest.mock import AsyncMock, patch

import pytest

from common.cache import RedisCache
from common.exceptions import MCPError
from fear_greed_mcp.infra.alternative_me import AlternativeMeClient
from fear_greed_mcp.service.fear_greed import FearGreedService

# ---------------------------------------------------------------------------
# Shared fixtures / helpers
# ---------------------------------------------------------------------------

_TODAY = "2026-03-19"
_YESTERDAY = "2026-03-18"

_RAW_ENTRY = {
    "value": "65",
    "value_classification": "Greed",
    "timestamp": "1742342400",
}

_PARSED_CURRENT = {
    "value": 65,
    "classification": "Greed",
    "updated_at": "2026-03-19T00:00:00Z",
}

_PARSED_HISTORICAL = {
    "value": 65,
    "classification": "Greed",
    "timestamp": "2026-03-19T00:00:00Z",
}


def _make_service(cached_value=None, fetch_raises=False):
    client = AsyncMock(spec=AlternativeMeClient)
    cache = AsyncMock(spec=RedisCache)

    if fetch_raises:
        client.get_index.side_effect = MCPError("upstream down")
    else:
        client.get_index.return_value = _RAW_ENTRY
        client.get_historical.return_value = [_RAW_ENTRY]

    cache.get.return_value = cached_value
    cache.set = AsyncMock()

    return FearGreedService(client, cache), client, cache


# ---------------------------------------------------------------------------
# get_index — cache scenarios
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("scenario,cached,fetch_raises,expect_fetch,expect_write,expect_error", [
    (
        "cache miss → fresh fetch + write",
        None, False, True, True, False,
    ),
    (
        "cache hit same day → no fetch",
        {"date": _TODAY, "data": _PARSED_CURRENT}, False, False, False, False,
    ),
    (
        "stale cache different day → refetch + overwrite",
        {"date": _YESTERDAY, "data": _PARSED_CURRENT}, False, True, True, False,
    ),
    (
        "upstream down + stale cache → MCPError (no silent fallback)",
        {"date": _YESTERDAY, "data": _PARSED_CURRENT}, True, True, False, True,
    ),
    (
        "upstream down + empty cache → MCPError",
        None, True, True, False, True,
    ),
])
async def test_get_index_scenarios(
    scenario, cached, fetch_raises, expect_fetch, expect_write, expect_error
):
    svc, client, cache = _make_service(cached_value=cached, fetch_raises=fetch_raises)

    with patch("fear_greed_mcp.service.fear_greed._utc_date_str", return_value=_TODAY):
        if expect_error:
            with pytest.raises(MCPError):
                await svc.get_index()
        else:
            result = await svc.get_index()
            assert result["value"] == 65
            assert result["classification"] == "Greed"

    assert client.get_index.called == expect_fetch, f"{scenario}: fetch call mismatch"
    assert cache.set.called == expect_write, f"{scenario}: cache write mismatch"


# ---------------------------------------------------------------------------
# get_historical — always fetches fresh
# ---------------------------------------------------------------------------

async def test_get_historical_always_fetches_fresh():
    svc, client, cache = _make_service()

    result1 = await svc.get_historical(1)
    await svc.get_historical(1)

    assert client.get_historical.call_count == 2
    cache.get.assert_not_called()
    cache.set.assert_not_called()
    assert result1[0]["value"] == 65


async def test_get_historical_returns_correct_shape():
    svc, client, _ = _make_service()
    client.get_historical.return_value = [_RAW_ENTRY, _RAW_ENTRY]

    result = await svc.get_historical(2)

    assert len(result) == 2
    assert set(result[0].keys()) == {"value", "classification", "timestamp"}
    assert isinstance(result[0]["value"], int)
