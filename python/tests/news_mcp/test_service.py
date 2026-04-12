"""Tests for NewsService — single-key cache logic, LLM call behavior, macro flag."""
from unittest.mock import AsyncMock

import pytest

from common.cache import RedisCache
from news_mcp.infra.base import NewsClient
from news_mcp.service.llm import AnalysisResult, NewsLLMScorer
from news_mcp.service.news import NewsService

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_ASSETS = ["BTC", "ETH"]
_TRACKED = ["BTC", "ETH", "SOL"]
_TS = "2026-03-20T10:00:00Z"


def _post(title: str, url: str = "", source: str = "") -> dict:
    return {"title": title, "description": "", "url": url, "published_at": _TS, "source": source}


_POSTS = [
    _post("BTC rallies", "u1", "CoinDesk"),
    _post("ETH upgrade", "u2", "Decrypt"),
    _post("Crypto surge", "u3", "Block"),
]

_NEUTRAL_ANALYSIS = AnalysisResult(
    sentiment={"BTC": 0.0, "ETH": 0.0}, macro_flag=False, macro_reason=None
)
_BULLISH_ANALYSIS = AnalysisResult(
    sentiment={"BTC": 0.8, "ETH": 0.3}, macro_flag=True, macro_reason="ETF approved"
)


def _make_service(*, cache_returns=None, posts=None, analysis=None):
    """Build a NewsService with mocked dependencies.

    cache_returns: dict mapping cache key → return value (or None = miss)
    """
    cache_returns = cache_returns or {}

    client = AsyncMock(spec=NewsClient)
    client.get_posts.return_value = posts if posts is not None else _POSTS

    scorer = AsyncMock(spec=NewsLLMScorer)
    scorer.analyze.return_value = analysis if analysis is not None else _NEUTRAL_ANALYSIS

    cache = AsyncMock(spec=RedisCache)
    cache.get.side_effect = lambda key: cache_returns.get(key)
    cache.set = AsyncMock()

    return NewsService(client, scorer, cache, _TRACKED), client, scorer, cache


# ---------------------------------------------------------------------------
# get_headlines — single cache key
# ---------------------------------------------------------------------------

async def test_get_headlines_cache_miss_fetches_and_stores():
    svc, client, _, cache = _make_service()

    result = await svc.get_headlines()

    client.get_posts.assert_called_once_with(_TRACKED)
    assert isinstance(result, list)
    stored_key = cache.set.call_args[0][0]
    assert stored_key == "news:headlines:all"


async def test_get_headlines_cache_hit_skips_api():
    cached = [_post("cached headline")]
    svc, client, _, _ = _make_service(cache_returns={"news:headlines:all": cached})

    result = await svc.get_headlines()

    client.get_posts.assert_not_called()
    assert result == cached


# ---------------------------------------------------------------------------
# get_sentiment — per-asset analysis cache
# ---------------------------------------------------------------------------

async def test_get_sentiment_cache_miss_calls_llm_and_stores_per_asset():
    svc, _, scorer, cache = _make_service(analysis=_BULLISH_ANALYSIS)

    result = await svc.get_sentiment(["BTC", "ETH"])

    scorer.analyze.assert_called_once()
    assert result == {"BTC": 0.8, "ETH": 0.3}

    stored_keys = {c[0][0] for c in cache.set.call_args_list}
    assert "news:analysis:BTC" in stored_keys
    assert "news:analysis:ETH" in stored_keys


async def test_get_sentiment_cache_hit_skips_llm():
    cache_returns = {
        "news:analysis:BTC": {"sentiment": 0.5},
        "news:analysis:ETH": {"sentiment": -0.2},
        "news:analysis:macro": {"triggered": False, "reason": None},
    }
    svc, _, scorer, _ = _make_service(cache_returns=cache_returns)

    result = await svc.get_sentiment(["BTC", "ETH"])

    scorer.analyze.assert_not_called()
    assert result["BTC"] == pytest.approx(0.5)
    assert result["ETH"] == pytest.approx(-0.2)


async def test_get_sentiment_partial_cache_hit_calls_llm_only_for_uncached():
    cache_returns = {"news:analysis:BTC": {"sentiment": 0.5}}
    eth_analysis = AnalysisResult(sentiment={"ETH": 0.1}, macro_flag=False, macro_reason=None)
    svc, _, scorer, _ = _make_service(cache_returns=cache_returns, analysis=eth_analysis)

    result = await svc.get_sentiment(["BTC", "ETH"])

    scorer.analyze.assert_called_once()
    called_assets = scorer.analyze.call_args[0][0]
    assert called_assets == ["ETH"]
    assert result["BTC"] == pytest.approx(0.5)
    assert result["ETH"] == pytest.approx(0.1)


async def test_get_sentiment_llm_receives_flat_headlines():
    svc, _, scorer, _ = _make_service(analysis=_BULLISH_ANALYSIS)

    await svc.get_sentiment(["BTC", "ETH"])

    _, headlines_arg = scorer.analyze.call_args[0]
    assert isinstance(headlines_arg, list)


# ---------------------------------------------------------------------------
# get_macro_flag
# ---------------------------------------------------------------------------

async def test_get_macro_flag_triggered():
    svc, _, scorer, _ = _make_service(analysis=_BULLISH_ANALYSIS)

    result = await svc.get_macro_flag(["BTC", "ETH"])

    assert result["triggered"] is True
    assert result["reason"] == "ETF approved"


async def test_get_macro_flag_not_triggered():
    svc, _, _, _ = _make_service(analysis=_NEUTRAL_ANALYSIS)

    result = await svc.get_macro_flag(["BTC", "ETH"])

    assert result["triggered"] is False
    assert result["reason"] is None


async def test_get_macro_flag_cache_hit_skips_llm():
    macro_cached = {"triggered": True, "reason": "Fed raised rates"}
    svc, _, scorer, _ = _make_service(cache_returns={"news:analysis:macro": macro_cached})

    result = await svc.get_macro_flag(["BTC", "ETH"])

    scorer.analyze.assert_not_called()
    assert result["triggered"] is True
    assert result["reason"] == "Fed raised rates"


# ---------------------------------------------------------------------------
# LLM called once when sentiment + macro share same assets
# ---------------------------------------------------------------------------

async def test_sentiment_and_macro_share_single_llm_call():
    """Calling get_sentiment then get_macro_flag with same assets triggers LLM only once."""
    svc, _, scorer, cache = _make_service(analysis=_BULLISH_ANALYSIS)

    await svc.get_sentiment(["BTC", "ETH"])
    cache.get.side_effect = lambda key: {
        "news:analysis:BTC": {"sentiment": 0.8},
        "news:analysis:ETH": {"sentiment": 0.3},
        "news:analysis:macro": {"triggered": True, "reason": "ETF approved"},
    }.get(key)

    result = await svc.get_macro_flag(["BTC", "ETH"])

    scorer.analyze.assert_called_once()
    assert result["triggered"] is True
