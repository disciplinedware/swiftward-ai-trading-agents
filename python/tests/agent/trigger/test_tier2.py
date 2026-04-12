from unittest.mock import AsyncMock, MagicMock

import pytest

from agent.trigger.tier2 import Tier2Loop
from common.models.signal_bundle import FearGreedData, NewsData

_TRACKED = ["BTC", "ETH", "SOL"]
_FG_LOW = 20
_FG_HIGH = 80


def _make_loop(
    fg_value: int = 50,
    macro_flag: bool = False,
) -> tuple[Tier2Loop, AsyncMock]:
    fear_greed = AsyncMock()
    fear_greed.get_index.return_value = FearGreedData(
        value=fg_value, classification="Neutral", timestamp=""
    )

    news = AsyncMock()
    news.get_signals.return_value = {
        asset: NewsData(sentiment=0.0, macro_flag=macro_flag)
        for asset in _TRACKED
    }

    clock = AsyncMock()
    clock._run_once = AsyncMock()

    config = MagicMock()
    config.trigger.fear_greed_low = _FG_LOW
    config.trigger.fear_greed_high = _FG_HIGH
    config.assets.tracked = _TRACKED

    loop = Tier2Loop(fear_greed=fear_greed, news=news, clock=clock, config=config)
    return loop, clock


# ── no conditions ──────────────────────────────────────────────────────────────

async def test_no_conditions_no_brain() -> None:
    loop, clock = _make_loop(fg_value=50, macro_flag=False)
    await loop._check()
    clock._run_once.assert_not_awaited()


# ── fear & greed threshold ─────────────────────────────────────────────────────

@pytest.mark.parametrize(
    "fg_value, expect_brain",
    [
        (19, True),   # below low threshold
        (20, False),  # exactly at low threshold — no trigger
        (21, False),  # just above low threshold
        (79, False),  # just below high threshold
        (80, False),  # exactly at high threshold — no trigger
        (81, True),   # above high threshold
        (0, True),    # extreme fear
        (100, True),  # extreme greed
    ],
)
async def test_fear_greed_threshold(fg_value: int, expect_brain: bool) -> None:
    loop, clock = _make_loop(fg_value=fg_value, macro_flag=False)
    await loop._check()

    if expect_brain:
        clock._run_once.assert_awaited_once()
    else:
        clock._run_once.assert_not_awaited()


# ── macro news flag ────────────────────────────────────────────────────────────

async def test_macro_flag_true_triggers_brain() -> None:
    loop, clock = _make_loop(fg_value=50, macro_flag=True)
    await loop._check()
    clock._run_once.assert_awaited_once()


async def test_macro_flag_false_no_brain() -> None:
    loop, clock = _make_loop(fg_value=50, macro_flag=False)
    await loop._check()
    clock._run_once.assert_not_awaited()


# ── deduplication ─────────────────────────────────────────────────────────────

async def test_both_conditions_fires_brain_once() -> None:
    """When both F&G and macro flag are true, brain fires exactly once."""
    loop, clock = _make_loop(fg_value=10, macro_flag=True)
    await loop._check()
    clock._run_once.assert_awaited_once()


# ── error resilience ───────────────────────────────────────────────────────────

async def test_fear_greed_fetch_error_skips_cycle() -> None:
    loop, clock = _make_loop()
    loop._fear_greed.get_index.side_effect = RuntimeError("timeout")
    await loop._check()  # must not raise
    clock._run_once.assert_not_awaited()


async def test_news_fetch_error_skips_cycle() -> None:
    loop, clock = _make_loop()
    loop._news.get_signals.side_effect = RuntimeError("timeout")
    await loop._check()  # must not raise
    clock._run_once.assert_not_awaited()
