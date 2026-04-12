"""Tests for agent.brain.deterministic_llm — Stage 1 (Market Filter)."""
from unittest.mock import AsyncMock, patch

import pytest

from agent.brain.deterministic_llm import DeterministicLLMBrain
from common.models.portfolio_snapshot import OpenPositionView, PortfolioSnapshot
from common.models.signal_bundle import (
    FearGreedData,
    NewsData,
    OnchainData,
    PriceFeedData,
    SignalBundle,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_config(
    ema=0.30, fear_greed=0.15, btc_trend=0.25, funding=0.20, volatility=0.10,
    risk_on=0.6, risk_off=0.4, clamp_pct=10.0,
    ar_momentum=0.40, ar_rel_strength=0.35, ar_volume=0.25, ar_held_bonus=0.05,
):
    from unittest.mock import MagicMock
    cfg = MagicMock()
    cfg.llm.base_url = "http://localhost:11434/v1"
    cfg.llm.model = "test"
    cfg.llm.api_key = "test"
    cfg.llm.max_tokens = 100
    cfg.llm.retries = 1
    cfg.brain.stage1.risk_on_threshold = risk_on
    cfg.brain.stage1.risk_off_threshold = risk_off
    cfg.brain.stage1.btc_trend_clamp_pct = clamp_pct
    cfg.brain.stage1.ema_steepness = 150.0
    cfg.brain.stage1.funding_peak_rate = 0.0001
    cfg.brain.stage1.funding_extreme_rate = 0.0005
    cfg.brain.stage1.macro_penalty_factor = 0.85
    cfg.brain.stage1.weights.ema = ema
    cfg.brain.stage1.weights.fear_greed = fear_greed
    cfg.brain.stage1.weights.btc_trend = btc_trend
    cfg.brain.stage1.weights.funding = funding
    cfg.brain.stage1.weights.volatility = volatility
    cfg.brain.stage2.weights.momentum = ar_momentum
    cfg.brain.stage2.weights.relative_strength = ar_rel_strength
    cfg.brain.stage2.weights.volume = ar_volume
    cfg.brain.stage2.held_asset_bonus = ar_held_bonus
    cfg.brain.stage2.max_selections = 2
    return cfg


def _price(
    price="50000", ema_50_1h="40000", change_24h="5.0",
    atr_14_15m="500", atr_chg_5="0",
) -> PriceFeedData:
    return PriceFeedData(
        price=price, ema_50_1h=ema_50_1h, change_24h=change_24h,
        atr_14_15m=atr_14_15m, atr_chg_5=atr_chg_5,
    )


def _onchain(funding_rate="0.0001") -> OnchainData:
    return OnchainData(funding_rate=funding_rate)


def _portfolio(open_positions=None) -> PortfolioSnapshot:
    return PortfolioSnapshot(
        total_usd="10000",
        stablecoin_balance="10000",
        open_position_count=len(open_positions or []),
        realized_pnl_today="0",
        current_drawdown_pct="0",
        open_positions=open_positions or [],
    )


def _bundle(
    btc_price="50000", btc_ema200="40000", btc_change_24h="5.0",
    fear_greed_val=70, funding_rate="0.0001", open_positions=None,
    atr_chg_5="0", news=None,
) -> SignalBundle:
    return SignalBundle(
        prices={"BTC": _price(btc_price, btc_ema200, btc_change_24h,
                              atr_chg_5=atr_chg_5)},
        fear_greed=FearGreedData(value=fear_greed_val, classification="Greed"),
        onchain={"BTC": _onchain(funding_rate)},
        news=news or {},
        portfolio=_portfolio(open_positions),
        trigger_reason="clock",
    )


def _brain(cfg=None) -> DeterministicLLMBrain:
    return DeterministicLLMBrain(cfg or _make_config())


# ---------------------------------------------------------------------------
# Health score calculation
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,kwargs,score_min,score_max", [
    (
        "all bullish",
        dict(btc_price="50000", btc_ema200="40000", btc_change_24h="8.0",
             fear_greed_val=80, funding_rate="0.0001"),
        0.6, 1.0,
    ),
    (
        "all bearish",
        dict(btc_price="30000", btc_ema200="40000", btc_change_24h="-8.0",
             fear_greed_val=20, funding_rate="-0.001"),
        0.0, 0.4,
    ),
    (
        "neutral / mixed",
        dict(btc_price="40000", btc_ema200="40000", btc_change_24h="0.0",
             fear_greed_val=50, funding_rate="0.0"),
        0.3, 0.7,
    ),
])
def test_health_score_range(name, kwargs, score_min, score_max):
    brain = _brain()
    bundle = _bundle(**kwargs)
    score, _ = brain._compute_health_score(bundle)
    assert score_min <= score <= score_max, f"{name}: score={score:.4f}"


def test_health_score_btc_absent_uses_neutral():
    brain = _brain()
    bundle = SignalBundle(
        prices={"ETH": _price("3000", "2500", "2.0")},  # no BTC
        fear_greed=FearGreedData(value=50, classification="Neutral"),
        onchain={},
        news={},
        portfolio=_portfolio(),
        trigger_reason="clock",
    )
    score, breakdown = brain._compute_health_score(bundle)
    assert breakdown["ema_signal"] == 0.5
    assert breakdown["btc_trend_norm"] == 0.5
    assert 0.0 <= score <= 1.0


def test_health_score_no_funding_data_uses_neutral():
    brain = _brain()
    bundle = SignalBundle(
        prices={"BTC": _price("50000", "40000", "3.0")},
        fear_greed=FearGreedData(value=60, classification="Greed"),
        onchain={"BTC": OnchainData(funding_rate=None)},
        news={},
        portfolio=_portfolio(),
        trigger_reason="clock",
    )
    score, breakdown = brain._compute_health_score(bundle)
    assert breakdown["funding_score"] == 0.5


# ---------------------------------------------------------------------------
# Verdict mapping
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("score,expected", [
    (0.75, "RISK_ON"),
    (0.61, "RISK_ON"),
    (0.60, "UNCERTAIN"),  # at threshold → UNCERTAIN
    (0.52, "UNCERTAIN"),
    (0.40, "UNCERTAIN"),  # at lower threshold → UNCERTAIN
    (0.39, "RISK_OFF"),
    (0.10, "RISK_OFF"),
])
def test_deterministic_verdict(score, expected):
    brain = _brain()
    assert brain._deterministic_verdict(score) == expected


# ---------------------------------------------------------------------------
# RISK_OFF short-circuit
# ---------------------------------------------------------------------------

@pytest.mark.skip(reason="FLAT_ALL on RISK_OFF disabled — stop-losses manage exits now")
async def test_risk_off_returns_flat_all_intent_for_open_positions():
    brain = _brain()
    positions = [
        OpenPositionView(asset="SOL", entry_price="100", stop_loss="90",
                         take_profit="130", size_pct="0.05", strategy="trend_following"),
        OpenPositionView(asset="AVAX", entry_price="20", stop_loss="18",
                         take_profit="26", size_pct="0.05", strategy="trend_following"),
    ]
    bundle = _bundle(btc_price="30000", btc_ema200="40000", btc_change_24h="-9.0",
                     fear_greed_val=15, funding_rate="-0.005", open_positions=positions)

    # Patch LLM to never be called
    never_called = AsyncMock(side_effect=AssertionError("LLM should not be called"))
    with patch.object(brain._llm, "call", new=never_called):
        intents, trace = await brain._stage1(bundle, "clock")

    assert trace["stage1_verdict"] == "RISK_OFF"
    assert len(intents) == 1
    assert intents[0].action == "FLAT_ALL"
    assert intents[0].asset is None


@pytest.mark.skip(reason="FLAT_ALL on RISK_OFF disabled — stop-losses manage exits now")
async def test_risk_off_no_positions_returns_flat_all():
    brain = _brain()
    bundle = _bundle(btc_price="30000", btc_ema200="40000", btc_change_24h="-9.0",
                     fear_greed_val=15, funding_rate="-0.005")
    never_called = AsyncMock(side_effect=AssertionError("should not call LLM"))
    with patch.object(brain._llm, "call", new=never_called):
        intents, trace = await brain._stage1(bundle, "clock")
    assert len(intents) == 1
    assert intents[0].action == "FLAT_ALL"
    assert trace["stage1_verdict"] == "RISK_OFF"


# ---------------------------------------------------------------------------
# LLM downgrade enforcement
# ---------------------------------------------------------------------------

async def _stage1_with_llm_verdict(llm_verdict: str, det_score: float = 0.75) -> tuple:
    """Helper: return (intents, trace) for a given LLM verdict and a score mapping to RISK_ON."""
    brain = _brain()
    # High score → det verdict RISK_ON
    bundle = _bundle(btc_price="60000", btc_ema200="40000", btc_change_24h="8.0",
                     fear_greed_val=80, funding_rate="0.0001")
    llm_mock = AsyncMock(return_value=("analysis", {"verdict": llm_verdict, "reason": "test"}))
    with patch.object(brain._llm, "call", new=llm_mock):
        intents, trace = await brain._stage1(bundle, "clock")
    return intents, trace


async def test_llm_downgrade_risk_on_to_uncertain_accepted():
    _, trace = await _stage1_with_llm_verdict("UNCERTAIN")
    assert trace["stage1_verdict"] == "UNCERTAIN"
    assert trace["uncertainty_multiplier"] == 0.5


async def test_llm_downgrade_risk_on_to_risk_off_accepted():
    intents, trace = await _stage1_with_llm_verdict("RISK_OFF")
    assert trace["stage1_verdict"] == "RISK_OFF"


async def test_llm_upgrade_clamped_to_deterministic():
    """Det=UNCERTAIN, LLM says RISK_ON → clamped to UNCERTAIN."""
    brain = _brain()
    bundle = _bundle(btc_price="40000", btc_ema200="40000", btc_change_24h="0.0",
                     fear_greed_val=50, funding_rate="0.0")
    # Score will be ~0.5 → UNCERTAIN
    upgrade_mock = AsyncMock(
        return_value=("reasoning", {"verdict": "RISK_ON", "reason": "trying to upgrade"})
    )
    with patch.object(brain._llm, "call", new=upgrade_mock):
        _, trace = await brain._stage1(bundle, "clock")
    assert trace["stage1_verdict"] == "UNCERTAIN"


async def test_llm_error_falls_back_to_deterministic():
    brain = _brain()
    bundle = _bundle(btc_price="60000", btc_ema200="40000", btc_change_24h="8.0",
                     fear_greed_val=80, funding_rate="0.0001")
    err_mock = AsyncMock(side_effect=Exception("connection refused"))
    with patch.object(brain._llm, "call", new=err_mock):
        _, trace = await brain._stage1(bundle, "clock")
    assert trace["stage1_verdict"] == "RISK_ON"  # deterministic fallback


# ---------------------------------------------------------------------------
# EMA sigmoid continuity
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,price,ema,expected_min,expected_max", [
    ("3% above EMA", "41200", "40000", 0.95, 1.0),
    ("1% above EMA", "40400", "40000", 0.7, 0.9),
    ("at EMA", "40000", "40000", 0.45, 0.55),
    ("1% below EMA", "39600", "40000", 0.1, 0.3),
    ("3% below EMA", "38800", "40000", 0.0, 0.05),
])
def test_ema_sigmoid_continuity(name, price, ema, expected_min, expected_max):
    brain = _brain()
    bundle = _bundle(btc_price=price, btc_ema200=ema)
    _, breakdown = brain._compute_health_score(bundle)
    sig = breakdown["ema_signal"]
    assert expected_min <= sig <= expected_max, (
        f"{name}: ema_signal={sig:.4f}"
    )


# ---------------------------------------------------------------------------
# Funding inverted-U
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,rate,expected_min,expected_max", [
    ("optimal positive", "0.0001", 0.95, 1.0),
    ("zero rate", "0.0", 0.45, 0.55),
    ("extreme positive", "0.0005", 0.25, 0.35),
    ("moderate negative", "-0.00015", 0.45, 0.55),
    ("extreme negative", "-0.0003", -0.01, 0.05),
])
def test_funding_inverted_u(name, rate, expected_min, expected_max):
    brain = _brain()
    bundle = _bundle(funding_rate=rate)
    _, breakdown = brain._compute_health_score(bundle)
    fs = breakdown["funding_score"]
    assert expected_min <= fs <= expected_max, (
        f"{name}: funding_score={fs:.4f}"
    )


# ---------------------------------------------------------------------------
# Volatility signal
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,atr_chg,expected_min,expected_max", [
    ("stable", "0", 0.75, 0.85),
    ("mild expansion", "5", 0.75, 0.85),
    ("high expansion", "60", 0.15, 0.40),
    ("mild contraction", "-5", 0.75, 0.85),
    ("extreme contraction", "-80", 0.35, 0.45),
])
def test_volatility_signal(name, atr_chg, expected_min, expected_max):
    brain = _brain()
    bundle = _bundle(atr_chg_5=atr_chg)
    _, breakdown = brain._compute_health_score(bundle)
    vs = breakdown["volatility_signal"]
    assert expected_min <= vs <= expected_max, (
        f"{name}: volatility_signal={vs:.4f}"
    )


# ---------------------------------------------------------------------------
# Macro penalty
# ---------------------------------------------------------------------------

def test_macro_flag_reduces_score():
    brain = _brain()
    bundle_normal = _bundle()
    bundle_macro = _bundle(news={"BTC": NewsData(macro_flag=True)})
    score_normal, _ = brain._compute_health_score(bundle_normal)
    score_macro, bd = brain._compute_health_score(bundle_macro)
    assert score_macro < score_normal
    assert score_macro == pytest.approx(score_normal * 0.85, abs=0.01)
    assert bd["macro_penalty_applied"] is True


def test_no_macro_flag_no_penalty():
    brain = _brain()
    _, bd = brain._compute_health_score(_bundle())
    assert bd["macro_penalty_applied"] is False


# ---------------------------------------------------------------------------
# Uncertainty multiplier
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("llm_verdict,expected_multiplier", [
    ("RISK_ON", 1.0),
    ("UNCERTAIN", 0.5),
])
async def test_uncertainty_multiplier(llm_verdict, expected_multiplier):
    brain = _brain()
    bundle = _bundle(btc_price="60000", btc_ema200="40000", btc_change_24h="8.0",
                     fear_greed_val=80, funding_rate="0.0001")
    verdict_mock = AsyncMock(return_value=("r", {"verdict": llm_verdict, "reason": "x"}))
    with patch.object(brain._llm, "call", new=verdict_mock):
        _, trace = await brain._stage1(bundle, "clock")
    assert trace["uncertainty_multiplier"] == expected_multiplier


# ---------------------------------------------------------------------------
# Factory integration
# ---------------------------------------------------------------------------

def test_factory_returns_deterministic_llm_brain():
    from agent.brain.base import Brain
    from agent.brain.factory import make_brain
    cfg = _make_config()
    cfg.brain.implementation = "deterministic_llm"
    brain = make_brain(cfg)
    assert isinstance(brain, DeterministicLLMBrain)
    assert isinstance(brain, Brain)


# ===========================================================================
# Stage 2 — Rotation Selector
# ===========================================================================

from agent.brain.deterministic_llm import (  # noqa: E402
    Stage1Trace,
    _apply_btc_eth_filter,
)


def _price_full(
    price="100", change_1h="1.0", change_4h="3.0", change_24h="5.0",
    rsi_14_15m="55", ema_20_15m="99", ema_50_15m="95", volume_ratio_15m="1.5",
    bb_upper_15m="105", bb_mid_15m="100", bb_lower_15m="95",
) -> PriceFeedData:
    return PriceFeedData(
        price=price, change_1h=change_1h, change_4h=change_4h, change_24h=change_24h,
        rsi_14_15m=rsi_14_15m, ema_20_15m=ema_20_15m, ema_50_15m=ema_50_15m,
        volume_ratio_15m=volume_ratio_15m,
        bb_upper_15m=bb_upper_15m, bb_mid_15m=bb_mid_15m, bb_lower_15m=bb_lower_15m,
    )


def _s1_trace(verdict="RISK_ON", multiplier=1.0) -> Stage1Trace:
    return Stage1Trace(  # type: ignore[call-arg]
        market_health_score=0.7,
        signal_breakdown={},
        trigger_reason="clock",
        stage1_verdict=verdict,
        stage1_reasoning="ok",
        uncertainty_multiplier=multiplier,
    )


def _bundle_multi(prices: dict) -> SignalBundle:
    return SignalBundle(
        prices=prices,
        fear_greed=FearGreedData(value=65, classification="Greed"),
        onchain={},
        news={},
        portfolio=_portfolio(),
        trigger_reason="clock",
    )


# ---------------------------------------------------------------------------
# Asset score math
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,change_4h,btc_4h,vol_ratio,expect_above_neutral", [
    ("strong outperformer", 8.0, 2.0, 2.0, True),
    ("underperformer", -5.0, 3.0, 0.5, False),
    ("neutral inline with BTC", 3.0, 3.0, 1.0, None),  # near 0.5
])
def test_asset_score_relative_to_neutral(name, change_4h, btc_4h, vol_ratio, expect_above_neutral):
    brain = _brain()
    data = _price_full(change_4h=str(change_4h), volume_ratio_15m=str(vol_ratio))
    score = brain._score_asset("ETH", data, btc_change_4h=btc_4h, held_assets=set())
    if expect_above_neutral is True:
        assert score > 0.5, f"{name}: expected score > 0.5, got {score:.4f}"
    elif expect_above_neutral is False:
        assert score < 0.5, f"{name}: expected score < 0.5, got {score:.4f}"
    else:
        assert 0.3 <= score <= 0.7, f"{name}: expected near-neutral, got {score:.4f}"


def test_held_asset_gets_bonus():
    brain = _brain()
    data = _price_full(change_4h="3.0", volume_ratio_15m="1.0")
    score_no_hold = brain._score_asset("ETH", data, btc_change_4h=3.0, held_assets=set())
    score_held = brain._score_asset("ETH", data, btc_change_4h=3.0, held_assets={"ETH"})
    assert score_held - score_no_hold == pytest.approx(0.05, abs=1e-9)


def test_rel_strength_neutral_when_btc_missing():
    brain = _brain()
    # _price_full defaults: change_1h="1.0", change_4h="5.0", change_24h="5.0"
    data = _price_full(change_4h="5.0", volume_ratio_15m="1.0")
    score_no_btc = brain._score_asset("SOL", data, btc_change_4h=None, held_assets=set())
    w = brain._cfg.brain.stage2.weights
    from agent.brain.deterministic_llm import _norm_change
    momentum = (
        0.5 * _norm_change(5.0, 20.0)
        + 0.3 * _norm_change(1.0, 20.0)   # change_1h default
        + 0.2 * _norm_change(5.0, 20.0)   # change_24h default
    )
    expected = momentum * w.momentum + 0.5 * w.relative_strength + (1.0 / 3.0) * w.volume
    assert score_no_btc == pytest.approx(expected, abs=1e-9)


# ---------------------------------------------------------------------------
# Top-N candidate selection
# ---------------------------------------------------------------------------

async def test_all_assets_passed_to_llm_ordered_by_score():
    brain = _brain()
    prices = {
        "BTC": _price_full(change_4h="8.0", volume_ratio_15m="2.0"),
        "ETH": _price_full(change_4h="7.0", volume_ratio_15m="2.0"),
        "SOL": _price_full(change_4h="6.0", volume_ratio_15m="2.0"),
        "AVAX": _price_full(change_4h="5.0", volume_ratio_15m="2.0"),
        "LINK": _price_full(change_4h="-5.0", volume_ratio_15m="0.5"),
    }
    bundle = _bundle_multi(prices)

    with patch.object(brain._llm, "call", new=AsyncMock(
        return_value=("r", {"selections": [{"asset": "BTC", "regime": "STRONG_UPTREND"}]})
    )):
        _, trace = await brain._stage2(bundle, _s1_trace())

    # all 5 assets present, ordered best-first
    assert trace["top_candidates"] == ["BTC", "ETH", "SOL", "AVAX", "LINK"]


# ---------------------------------------------------------------------------
# BTC/ETH correlation filter
# ---------------------------------------------------------------------------

_BTC_UP = {"asset": "BTC", "regime": "STRONG_UPTREND"}
_ETH_BK = {"asset": "ETH", "regime": "BREAKOUT"}
_SOL_BK = {"asset": "SOL", "regime": "BREAKOUT"}
_ETH_RG = {"asset": "ETH", "regime": "RANGING"}


@pytest.mark.parametrize("name,raw_selected,expected_assets", [
    ("btc+eth → drop eth", [_BTC_UP, _ETH_BK], ["BTC"]),
    ("btc+sol → keep both", [_BTC_UP, _SOL_BK], ["BTC", "SOL"]),
    ("eth+sol → keep both", [_ETH_RG, _SOL_BK], ["ETH", "SOL"]),
    ("only eth → keep", [_ETH_RG], ["ETH"]),
])
def test_btc_eth_filter(name, raw_selected, expected_assets):
    result = _apply_btc_eth_filter(raw_selected)
    assert [s["asset"] for s in result] == expected_assets, name


# ---------------------------------------------------------------------------
# LLM re-ranking — validation
# ---------------------------------------------------------------------------

_TOP4 = ["BTC", "ETH", "SOL", "AVAX"]
_PRICES4 = {a: _price_full() for a in _TOP4}



@pytest.mark.parametrize("name,llm_response", [
    ("llm error", Exception("timeout")),
    ("empty list", {"selections": []}),
    ("missing key", {"other": "data"}),
    ("unknown asset", {"selections": [{"asset": "DOGE", "regime": "STRONG_UPTREND"}]}),
    ("invalid regime", {"selections": [{"asset": "BTC", "regime": "MOON_SHOT"}]}),
    ("non-dict item", {"selections": ["BTC"]}),
])
async def test_llm_stage2_aborts_on_bad_response(name, llm_response):
    from agent.brain.errors import BrainError
    brain = _brain()
    bundle = _bundle_multi(_PRICES4)

    if isinstance(llm_response, Exception):
        mock = AsyncMock(side_effect=llm_response)
    else:
        mock = AsyncMock(return_value=("r", llm_response))

    with patch.object(brain._llm, "call", new=mock):
        with pytest.raises((BrainError, Exception)):
            await brain._call_llm_stage2(bundle, _TOP4, _s1_trace())


# ---------------------------------------------------------------------------
# Stage2 trace content
# ---------------------------------------------------------------------------

async def test_stage2_reasoning_captured_in_trace():
    brain = _brain()
    bundle = _bundle_multi(_PRICES4)

    reasoning_mock = AsyncMock(return_value=(
        "deep market analysis here",
        {"selections": [{"asset": "BTC", "regime": "STRONG_UPTREND"}]},
    ))
    with patch.object(brain._llm, "call", new=reasoning_mock):
        _, trace = await brain._stage2(bundle, _s1_trace())

    assert trace["stage2_reasoning"] == "deep market analysis here"
    assert "BTC" in trace["top_candidates"]
    assert trace["selected"][0]["asset"] == "BTC"


# ===========================================================================
# Stage 3 — Decision Engine
# ===========================================================================

from decimal import Decimal  # noqa: E402

from agent.brain.deterministic_llm import (  # noqa: E402
    _ACTION_ORDER,
    Stage2Trace,
    validate_trade_intent,
)
from common.models.trade_intent import TradeIntent  # noqa: E402


def _make_config_stage3(**overrides):
    from unittest.mock import MagicMock

    from common.config import RegimeSlTpConfig
    cfg = _make_config()
    cfg.brain.stage3.half_kelly_fraction = overrides.get("half_kelly_fraction", 0.09)
    cfg.brain.stage3.min_reward_risk_ratio = overrides.get("min_reward_risk_ratio", 2.0)
    cfg.assets.tracked = overrides.get("tracked", ["BTC", "ETH", "SOL", "BNB", "AVAX"])
    rm = MagicMock()
    rm.STRONG_UPTREND = 1.0
    rm.BREAKOUT = 0.75
    rm.RANGING = 0.5
    rm.WEAK_MIXED = 0.25
    cfg.brain.stage3.regime_multipliers = rm
    cfg.brain.stage3.regime_sl_tp = overrides.get("regime_sl_tp", {
        "STRONG_UPTREND": RegimeSlTpConfig(sl_mult=2.0, tp_mult=4.0),
        "BREAKOUT":       RegimeSlTpConfig(sl_mult=1.5, tp_mult=4.5),
        "RANGING":        RegimeSlTpConfig(sl_mult=1.5, tp_mult=3.0),
        "WEAK_MIXED":     RegimeSlTpConfig(sl_mult=1.5, tp_mult=3.0),
    })
    return cfg


def _price_s3(price="50000", atr_14_15m="500") -> PriceFeedData:
    return PriceFeedData(price=price, atr_14_15m=atr_14_15m, ema_50_1h="40000", change_24h="5.0")


def _s2_trace(selected=None) -> Stage2Trace:
    return Stage2Trace(  # type: ignore[call-arg]
        candidate_scores={},
        top_candidates=[],
        selected=selected or [],
        stage2_reasoning="good breakout setup",
    )


def _bundle_s3(prices: dict, open_positions=None) -> SignalBundle:
    return SignalBundle(
        prices=prices,
        fear_greed=FearGreedData(value=65, classification="Greed"),
        onchain={},
        news={},
        portfolio=_portfolio(open_positions),
        trigger_reason="clock",
    )


# ---------------------------------------------------------------------------
# Happy path — all four regimes
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("regime,expected_size,expected_tag,expected_sl,expected_tp", [
    # entry=50000, atr=500; sl/tp from regime_sl_tp config
    ("STRONG_UPTREND", Decimal("0.09"),   "trend_following", Decimal("49000"), Decimal("52000")),
    ("BREAKOUT",       Decimal("0.0675"), "breakout",        Decimal("49250"), Decimal("52250")),
    ("RANGING",        Decimal("0.045"),  "mean_reversion",  Decimal("49250"), Decimal("51500")),
    ("WEAK_MIXED",     Decimal("0.0225"), "mean_reversion",  Decimal("49250"), Decimal("51500")),
])
def test_stage3_happy_path(regime, expected_size, expected_tag, expected_sl, expected_tp):
    cfg = _make_config_stage3()
    brain = _brain(cfg)
    bundle = _bundle_s3({"SOL": _price_s3("50000", "500")})
    selected = [{"asset": "SOL", "regime": regime}]

    intents, trace = brain._stage3(bundle, _s1_trace("RISK_ON", 1.0), selected, _s2_trace(selected))

    assert len(intents) == 1, f"{regime}: expected 1 intent, got {trace}"
    intent = intents[0]
    assert intent.action == "LONG"
    assert intent.asset == "SOL"
    assert intent.strategy == expected_tag
    assert intent.size_pct == expected_size
    assert intent.stop_loss == expected_sl
    assert intent.take_profit == expected_tp
    assert "SOL" in intent.reasoning
    assert trace["intents_produced"] == 1



# ---------------------------------------------------------------------------
# Skip cases
# ---------------------------------------------------------------------------

def test_stage3_skip_atr_zero():
    cfg = _make_config_stage3()
    brain = _brain(cfg)
    bundle = _bundle_s3({"SOL": _price_s3("50000", "0")})
    selected = [{"asset": "SOL", "regime": "STRONG_UPTREND"}]

    intents, trace = brain._stage3(bundle, _s1_trace(), selected, _s2_trace())

    assert intents == []
    assert "SOL" in trace["skipped_atr_zero"]


def test_stage3_skip_rr_too_low():
    # entry=50000, atr=500 → R:R=2.0; raise min to 3.0 to force skip
    cfg = _make_config_stage3(min_reward_risk_ratio=3.0)
    brain = _brain(cfg)
    bundle = _bundle_s3({"SOL": _price_s3("50000", "500")})
    selected = [{"asset": "SOL", "regime": "STRONG_UPTREND"}]

    intents, trace = brain._stage3(bundle, _s1_trace(), selected, _s2_trace())

    assert intents == []
    assert "SOL" in trace["skipped_rr"]


def test_stage3_skip_already_held():
    cfg = _make_config_stage3()
    brain = _brain(cfg)
    position = OpenPositionView(
        asset="SOL", entry_price="49000", stop_loss="48000",
        take_profit="52000", size_pct="0.09", strategy="trend_following",
    )
    bundle = _bundle_s3({"SOL": _price_s3("50000", "500")}, open_positions=[position])
    selected = [{"asset": "SOL", "regime": "STRONG_UPTREND"}]

    intents, trace = brain._stage3(bundle, _s1_trace(), selected, _s2_trace())

    assert intents == []
    assert "SOL" in trace["skipped_held"]


# ---------------------------------------------------------------------------
# validate_trade_intent
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name,intent_kw,entry,expect_valid", [
    (
        "valid LONG",
        dict(asset="SOL", action="LONG", size_pct="0.09", stop_loss="49250",
             take_profit="51500", strategy="trend_following",
             reasoning="ok", trigger_reason="clock", confidence=0.8),
        Decimal("50000"),
        True,
    ),
    (
        "stop_loss >= entry",
        dict(asset="SOL", action="LONG", size_pct="0.09", stop_loss="50001",
             take_profit="51500", strategy="trend_following",
             reasoning="ok", trigger_reason="clock", confidence=0.8),
        Decimal("50000"),
        False,
    ),
    (
        "take_profit <= entry",
        dict(asset="SOL", action="LONG", size_pct="0.09", stop_loss="49250",
             take_profit="49999", strategy="trend_following",
             reasoning="ok", trigger_reason="clock", confidence=0.8),
        Decimal("50000"),
        False,
    ),
    (
        "asset not in tracked list",
        dict(asset="DOGE", action="LONG", size_pct="0.09", stop_loss="0.09",
             take_profit="0.15", strategy="trend_following",
             reasoning="ok", trigger_reason="clock", confidence=0.8),
        Decimal("0.10"),
        False,
    ),
    (
        "size_pct zero",
        dict(asset="SOL", action="LONG", size_pct="0.001", stop_loss="49250",
             take_profit="51500", strategy="trend_following",
             reasoning="ok", trigger_reason="clock", confidence=0.8),
        Decimal("50000"),
        True,  # 0.001 is within (0, 1.0]
    ),
    (
        "size_pct above max",
        dict(asset="SOL", action="LONG", size_pct="1.5", stop_loss="49250",
             take_profit="51500", strategy="trend_following",
             reasoning="ok", trigger_reason="clock", confidence=0.8),
        Decimal("50000"),
        False,
    ),
    (
        "valid FLAT",
        dict(asset="SOL", action="FLAT", size_pct="0",
             strategy="trend_following", reasoning="exit", trigger_reason="clock",
             confidence=1.0),
        None,
        True,
    ),
    (
        "FLAT unknown asset",
        dict(asset="DOGE", action="FLAT", size_pct="0",
             strategy="trend_following", reasoning="exit", trigger_reason="clock",
             confidence=1.0),
        None,
        False,
    ),
])
def test_validate_trade_intent(name, intent_kw, entry, expect_valid):
    intent = TradeIntent(**intent_kw)
    violations = validate_trade_intent(
        intent,
        tracked_assets=["BTC", "ETH", "SOL", "BNB", "AVAX"],
        min_rr=2.0,
        max_size=Decimal("1.0"),
        entry_price=entry,
    )
    if expect_valid:
        assert violations == [], f"{name}: expected no violations, got {violations}"
    else:
        assert violations, f"{name}: expected violations, got none"


# ---------------------------------------------------------------------------
# Ordering: FLAT before LONG
# ---------------------------------------------------------------------------

def test_action_order_flat_before_long():
    assert _ACTION_ORDER["FLAT_ALL"] < _ACTION_ORDER["FLAT"] < _ACTION_ORDER["LONG"]


def test_stage3_multiple_assets_all_long():
    # entry=100, atr=2 → sl=97, tp=106, R:R=2.0 (passes)
    cfg = _make_config_stage3()
    brain = _brain(cfg)
    bundle = _bundle_s3({
        "SOL": _price_s3("100", "2"),
        "BNB": _price_s3("300", "6"),
    })
    selected = [
        {"asset": "SOL", "regime": "STRONG_UPTREND"},
        {"asset": "BNB", "regime": "BREAKOUT"},
    ]

    intents, trace = brain._stage3(bundle, _s1_trace(), selected, _s2_trace(selected))

    assert len(intents) == 2
    assert all(i.action == "LONG" for i in intents)
    assert trace["intents_produced"] == 2
    assets = {i.asset for i in intents}
    assert assets == {"SOL", "BNB"}
