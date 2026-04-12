"""DeterministicLLMBrain — three-stage trading brain.

Stage 1: Market Filter — deterministic health score + LLM downgrade.
Stage 2: Rotation Selector — deterministic ranker + LLM re-ranking + regime assignment.
Stage 3: Decision Engine — ATR-based stops, Kelly sizing, TradeIntent assembly.
"""

import math
from decimal import Decimal
from typing import Literal, TypedDict, cast

from agent.brain.errors import BrainError
from agent.brain.llm_client import LLMClient
from common.config import AgentConfig
from common.log import get_logger
from common.models.signal_bundle import PriceFeedData, SignalBundle
from common.models.trade_intent import StrategyTag, TradeIntent, TriggerReason

logger = get_logger(__name__)

# ---------------------------------------------------------------------------
# Shared constants / helpers
# ---------------------------------------------------------------------------

Verdict = Literal["RISK_OFF", "UNCERTAIN", "RISK_ON"]
Regime = Literal["STRONG_UPTREND", "BREAKOUT", "RANGING", "WEAK_MIXED"]

# Verdict ordering: lower index = more conservative
_VERDICT_ORDER = {"RISK_OFF": 0, "UNCERTAIN": 1, "RISK_ON": 2}
_VALID_VERDICTS = set(_VERDICT_ORDER)

# Stage 2 constants
# STRONG_UPTREND — price firmly above key MAs with sustained momentum;
#                  strategy: trend_following at full size.
# BREAKOUT       — price breaking a resistance level on elevated volume;
#                  strategy: breakout with wider TP.
# RANGING        — price oscillates in a horizontal channel, no clear trend;
#                  strategy: mean_reversion.
# WEAK_MIXED     — conflicting signals, no clear market structure;
#                  strategy: mean_reversion at reduced size.
_VALID_REGIMES: frozenset[str] = frozenset({"STRONG_UPTREND", "BREAKOUT", "RANGING", "WEAK_MIXED"})


class SelectedAsset(TypedDict):
    asset: str
    regime: Regime


# Stage 3 constants
_REGIME_TO_STRATEGY: dict[Regime, StrategyTag] = {
    "STRONG_UPTREND": "trend_following",
    "BREAKOUT": "breakout",
    "RANGING": "mean_reversion",
    "WEAK_MIXED": "mean_reversion",
}

_ACTION_ORDER: dict[str, int] = {"FLAT_ALL": 0, "FLAT": 1, "LONG": 2}

# Confidence weighted average weights (sum = 1.0)
_CONF_W_HEALTH = 0.3       # Stage 1 market health score
_CONF_W_UNCERTAINTY = 0.2   # Stage 1 uncertainty multiplier
_CONF_W_REGIME = 0.3        # Stage 2 regime strength
_CONF_W_RR = 0.2            # Stage 3 R:R quality

_REGIME_CONFIDENCE: dict[str, float] = {
    "STRONG_UPTREND": 1.0,
    "BREAKOUT": 0.85,
    "RANGING": 0.6,
    "WEAK_MIXED": 0.4,
}


def _clamp(v: float, lo: float, hi: float) -> float:
    return max(lo, min(hi, v))


def _norm_change(pct: float, clamp: float) -> float:
    """Clamp a percentage change to [-clamp, clamp] and normalize to [0, 1]."""
    return (_clamp(pct / clamp, -1.0, 1.0) + 1.0) / 2.0



def _apply_btc_eth_filter(selected: list[SelectedAsset]) -> list[SelectedAsset]:
    """If both BTC and ETH are in the selection, drop ETH."""
    assets = {s["asset"] for s in selected}
    if "BTC" in assets and "ETH" in assets:
        return [s for s in selected if s["asset"] != "ETH"]
    return selected


# ---------------------------------------------------------------------------
# Trace TypedDicts
# ---------------------------------------------------------------------------


class Stage1Trace(TypedDict):
    market_health_score: float
    signal_breakdown: dict[str, float]
    trigger_reason: TriggerReason
    stage1_verdict: Verdict
    stage1_reasoning: str
    uncertainty_multiplier: float


class Stage2Trace(TypedDict):
    candidate_scores: dict[str, float]   # all scored assets
    top_candidates: list[str]            # top-N passed to LLM
    selected: list[SelectedAsset]
    stage2_reasoning: str


class Stage3Trace(TypedDict):
    intents_produced: int
    skipped_held: list[str]
    skipped_atr_zero: list[str]
    skipped_rr: list[str]
    skipped_validation: list[str]


# ---------------------------------------------------------------------------
# Stage 3 helpers (module-level for testability)
# ---------------------------------------------------------------------------


def validate_trade_intent(
    intent: TradeIntent,
    tracked_assets: list[str],
    min_rr: float,
    max_size: Decimal,
    entry_price: Decimal | None = None,
) -> list[str]:
    """Validate a TradeIntent before submission. Returns list of violation strings.

    An empty list means the intent is valid.
    """
    violations: list[str] = []

    if intent.action == "LONG":
        if intent.asset is None or intent.asset not in tracked_assets:
            violations.append(f"asset {intent.asset!r} not in tracked list")
        if intent.size_pct <= 0 or intent.size_pct > max_size:
            violations.append(
                f"size_pct {intent.size_pct} out of bounds (0, {max_size}]"
            )
        if entry_price is not None and intent.stop_loss is not None:
            if intent.stop_loss >= entry_price:
                violations.append(
                    f"stop_loss {intent.stop_loss} >= entry_price {entry_price}"
                )
            if intent.take_profit is not None and intent.take_profit <= entry_price:
                violations.append(
                    f"take_profit {intent.take_profit} <= entry_price {entry_price}"
                )
            if (
                intent.stop_loss is not None
                and intent.take_profit is not None
                and intent.stop_loss < entry_price
                and intent.take_profit > entry_price
            ):
                risk = entry_price - intent.stop_loss
                reward = intent.take_profit - entry_price
                rr = float(reward / risk)
                if rr < min_rr:
                    violations.append(
                        f"R:R {rr:.3f} below minimum {min_rr}"
                    )
    elif intent.action == "FLAT":
        if intent.asset is None or intent.asset not in tracked_assets:
            violations.append(f"FLAT asset {intent.asset!r} not in tracked list")

    return violations


def _build_stage3_reasoning(
    *,
    asset: str,
    regime: str,
    entry_price: Decimal,
    stop_loss: Decimal,
    take_profit: Decimal,
    size_pct: Decimal,
    rr: float,
    stage1_reasoning: str,
    stage2_reasoning: str,
) -> str:
    parts = [
        f"[Stage3] {asset} | regime={regime} | entry={entry_price} | "
        f"sl={stop_loss} | tp={take_profit} | size={size_pct} | R:R={round(rr, 2)}",
    ]
    if stage1_reasoning:
        parts.append(f"[Stage1] {stage1_reasoning}")
    if stage2_reasoning:
        parts.append(f"[Stage2] {stage2_reasoning}")
    return "\n".join(parts)


class DeterministicLLMBrain:
    """Brain implementation: deterministic pipeline + LLM as risk-tightening auditor."""

    def __init__(self, config: AgentConfig) -> None:
        self._cfg = config
        self._llm = LLMClient(
            base_url=config.llm.base_url,
            model=config.llm.model,
            api_key=config.llm.api_key,
            max_tokens=config.llm.max_tokens,
            retries=config.llm.retries,
        )

    # ------------------------------------------------------------------
    # Public interface (Brain protocol)
    # ------------------------------------------------------------------

    async def run(self, signal_bundle: SignalBundle) -> list[TradeIntent]:
        trigger_reason = signal_bundle.trigger_reason

        logger.info(
            "[brain] run starting",
            trigger=trigger_reason,
            assets=sorted(signal_bundle.prices.keys()),
            open_positions=[p.asset for p in signal_bundle.portfolio.open_positions],
            portfolio_usd=str(signal_bundle.portfolio.total_usd),
            stablecoin_balance=str(signal_bundle.portfolio.stablecoin_balance),
            fear_greed=signal_bundle.fear_greed.value,
            fear_greed_class=signal_bundle.fear_greed.classification,
        )

        # Stage 1 — Market Filter
        intents, stage1_trace = await self._stage1(signal_bundle, trigger_reason)
        if stage1_trace.get("stage1_verdict") == "RISK_OFF":
            logger.info("[brain] RISK_OFF exit", intents=[i.action for i in intents])
            return intents

        # Stage 2 — Rotation Selector
        selected, stage2_trace = await self._stage2(signal_bundle, stage1_trace)
        if not selected:
            logger.info("[brain] no assets selected")
            return []

        # Stage 3 — Decision Engine
        intents, _stage3_trace = self._stage3(signal_bundle, stage1_trace, selected, stage2_trace)
        logger.info(
            "[brain] run complete",
            intents=[
                {"action": i.action, "asset": i.asset, "size_pct": str(i.size_pct)}
                for i in intents
            ],
        )
        return intents

    # ------------------------------------------------------------------
    # Stage 1 — Market Filter
    # ------------------------------------------------------------------

    async def _stage1(
        self, signal_bundle: SignalBundle, trigger_reason: TriggerReason
    ) -> tuple[list[TradeIntent], Stage1Trace]:
        """Run Stage 1. Returns (intents, trace).

        If RISK_OFF: intents = FLAT_ALL, trace has stage1_verdict.
        Otherwise: intents = [], trace carries uncertainty_multiplier for Stage 3.
        """
        score, breakdown = self._compute_health_score(signal_bundle)
        det_verdict = self._deterministic_verdict(score)

        logger.info(
            "[stage 1] health score",
            score=round(score, 4),
            verdict=det_verdict,
            breakdown={k: round(v, 4) for k, v in breakdown.items()},
        )

        trace = cast(Stage1Trace, {
            "market_health_score": score,
            "signal_breakdown": breakdown,
            "trigger_reason": trigger_reason,
        })

        if det_verdict == "RISK_OFF":
            trace["stage1_verdict"] = "RISK_OFF"
            trace["stage1_reasoning"] = (
                f"Deterministic market filter returned RISK_OFF (score={score:.4f}, "
                f"threshold={self._cfg.brain.stage1.risk_off_threshold}). "
                f"Signals: ema={breakdown['ema_signal']:.2f}, "
                f"fear_greed={breakdown['fear_greed_norm']:.2f}, "
                f"btc_trend={breakdown['btc_trend_norm']:.2f}, "
                f"funding={breakdown['funding_score']:.2f}, "
                f"volatility={breakdown['volatility_signal']:.2f}, "
                f"macro={breakdown['macro_penalty_applied']}. "
                "LLM call skipped — blocking new entries only."
            )
            trace["uncertainty_multiplier"] = 0.0
            # Existing positions have stop-losses; no need to panic-sell on RISK_OFF
            # intents = self._flat_all_open_positions(
            #     trigger_reason, trace["stage1_reasoning"]
            # )
            return [], trace

        # Call LLM — downgrade only
        final_verdict, reasoning = await self._call_llm_stage1(
            score, breakdown, trigger_reason, det_verdict,
            signal_bundle,
        )

        trace["stage1_verdict"] = final_verdict
        trace["stage1_reasoning"] = reasoning
        trace["uncertainty_multiplier"] = 0.5 if final_verdict == "UNCERTAIN" else 1.0

        if final_verdict == "RISK_OFF":
            # Existing positions have stop-losses; no need to panic-sell on RISK_OFF
            # intents = self._flat_all_open_positions(
            #     trigger_reason, reasoning
            # )
            return [], trace

        return [], trace

    def _compute_health_score(self, bundle: SignalBundle) -> tuple[float, dict]:
        """Compute deterministic market health score in [0, 1].

        Returns (score, breakdown_dict).
        """
        mf = self._cfg.brain.stage1
        w = mf.weights
        clamp_pct = mf.btc_trend_clamp_pct

        # --- EMA50 1h sigmoid proximity (BTC as market proxy) ---
        btc = bundle.prices.get("BTC")
        if btc is not None:
            price = float(btc.price) if btc.price else 0.0
            ema50_1h = float(btc.ema_50_1h) if btc.ema_50_1h else 0.0
            if price > 0 and ema50_1h > 0:
                distance_pct = _clamp((price - ema50_1h) / ema50_1h, -0.5, 0.5)
                k = mf.ema_steepness
                ema_signal = 1.0 / (1.0 + math.exp(-k * distance_pct))
            else:
                ema_signal = 0.5
            logger.debug(
                "[stage 1] ema50_1h sigmoid",
                btc_price=price,
                btc_ema50_1h=ema50_1h,
                ema_signal=round(ema_signal, 4),
                weight=w.ema,
                contribution=round(ema_signal * w.ema, 4),
            )
        else:
            logger.warning("BTC missing from signal bundle — using neutral ema signal (0.5)")
            ema_signal = 0.5

        # --- Fear/Greed S-curve (amplifies extremes, compresses mid-range) ---
        fg_x = (bundle.fear_greed.value - 50) * 0.08
        fear_greed_norm = 1.0 / (1.0 + math.exp(-fg_x))
        logger.debug(
            "[stage 1] fear/greed",
            raw_value=bundle.fear_greed.value,
            classification=bundle.fear_greed.classification,
            normalized=round(fear_greed_norm, 4),
            weight=w.fear_greed,
            contribution=round(fear_greed_norm * w.fear_greed, 4),
        )

        # --- BTC 24h trend normalized ---
        if btc is not None:
            change_24h = float(btc.change_24h) if btc.change_24h else 0.0
            raw = _clamp(change_24h / clamp_pct, -1.0, 1.0)
            btc_trend_norm = (raw + 1.0) / 2.0
            logger.debug(
                "[stage 1] btc trend",
                btc_change_24h=change_24h,
                clamp_pct=clamp_pct,
                normalized=round(btc_trend_norm, 4),
                weight=w.btc_trend,
                contribution=round(btc_trend_norm * w.btc_trend, 4),
            )
        else:
            btc_trend_norm = 0.5

        # --- Funding score (inverted-U: penalizes extreme positive) ---
        funding_score = self._compute_funding_score(bundle)
        logger.debug(
            "[stage 1] funding score",
            funding_score=round(funding_score, 4),
            weight=w.funding,
            contribution=round(funding_score * w.funding, 4),
        )

        # --- Volatility regime (BTC ATR change) ---
        if btc is not None:
            btc_atr_chg = float(btc.atr_chg_5) if btc.atr_chg_5 else 0.0
            clamped_atr = _clamp(btc_atr_chg, -100.0, 100.0)
            if abs(clamped_atr) <= 10.0:
                volatility_signal = 0.8
            elif clamped_atr > 10.0:
                volatility_signal = max(0.2, 0.8 - 0.6 * (clamped_atr - 10.0) / 70.0)
            else:
                volatility_signal = max(0.4, 0.8 - 0.4 * (abs(clamped_atr) - 10.0) / 70.0)
            logger.debug(
                "[stage 1] volatility",
                btc_atr_chg_5=btc_atr_chg,
                volatility_signal=round(volatility_signal, 4),
                weight=w.volatility,
                contribution=round(volatility_signal * w.volatility, 4),
            )
        else:
            volatility_signal = 0.5

        score = (
            ema_signal          * w.ema
            + fear_greed_norm   * w.fear_greed
            + btc_trend_norm    * w.btc_trend
            + funding_score     * w.funding
            + volatility_signal * w.volatility
        )

        # --- Macro event penalty (FOMC, CPI, etc.) ---
        macro_applied = any(nd.macro_flag for nd in bundle.news.values())
        if macro_applied:
            score = _clamp(score * mf.macro_penalty_factor, 0.0, 1.0)
            logger.info("[stage 1] macro penalty applied", factor=mf.macro_penalty_factor)

        breakdown = {
            "ema_signal": ema_signal,
            "fear_greed_norm": fear_greed_norm,
            "btc_trend_norm": btc_trend_norm,
            "funding_score": funding_score,
            "volatility_signal": volatility_signal,
            "macro_penalty_applied": macro_applied,
        }
        return score, breakdown

    def _compute_funding_score(self, bundle: SignalBundle) -> float:
        """Average per-asset funding score across assets with non-null funding_rate.

        Inverted-U shape: peak at funding_peak_rate, declining toward 0.3 at
        funding_extreme_rate. Negative rates linearly interpolate to 0.0 at -0.03%.
        Returns 0.5 if no funding data available.
        """
        mf = self._cfg.brain.stage1
        peak_rate = mf.funding_peak_rate
        extreme_rate = mf.funding_extreme_rate

        scores: list[float] = []
        per_asset: dict[str, float] = {}
        for asset, onchain in bundle.onchain.items():
            if onchain.funding_rate is None:
                continue
            try:
                rate = float(onchain.funding_rate)
            except (ValueError, TypeError):
                continue
            if rate >= 0:
                if rate <= peak_rate:
                    asset_score = 0.5 + 0.5 * (rate / peak_rate) if peak_rate > 0 else 0.5
                else:
                    denom = extreme_rate - peak_rate
                    overshoot = (rate - peak_rate) / denom if denom > 0 else 1.0
                    asset_score = max(0.3, 1.0 - 0.7 * overshoot)
            else:
                threshold = -0.0003
                asset_score = _clamp(1.0 + rate / abs(threshold), 0.0, 1.0)
            scores.append(asset_score)
            per_asset[asset] = round(asset_score, 4)

        if not scores:
            logger.debug("[stage 1] funding: no data, using neutral 0.5")
            return 0.5

        avg = sum(scores) / len(scores)
        logger.debug("[stage 1] funding per-asset", per_asset=per_asset, avg=round(avg, 4))
        return avg

    def _deterministic_verdict(self, score: float) -> Verdict:
        mf = self._cfg.brain.stage1
        if score > mf.risk_on_threshold:
            return "RISK_ON"
        if score < mf.risk_off_threshold:
            return "RISK_OFF"
        return "UNCERTAIN"

    async def _call_llm_stage1(
        self,
        score: float,
        breakdown: dict,
        trigger_reason: str,
        det_verdict: Verdict,
        signal_bundle: SignalBundle,
    ) -> tuple[Verdict, str]:
        """Call LLM for Stage 1 verdict. Returns (final_verdict, reasoning).

        The LLM can only downgrade, never upgrade. Falls back to det_verdict on error.
        """
        system, user = self._build_stage1_prompt(
            score, breakdown, trigger_reason, det_verdict, signal_bundle,
        )
        logger.debug(
            "[stage 1] calling LLM",
            det_verdict=det_verdict,
            score=round(score, 4),
            user=user,
        )
        try:
            reasoning, decision = await self._llm.call(system, user)
        except Exception as exc:
            logger.error("LLM call failed in stage1 — using deterministic verdict", error=str(exc))
            return det_verdict, ""

        logger.debug(
            "[stage 1] LLM response",
            decision=decision,
            reasoning_preview=reasoning if reasoning else "",
        )

        raw_verdict = decision.get("verdict", det_verdict)
        if raw_verdict not in _VALID_VERDICTS:
            logger.warning("LLM returned invalid verdict, ignoring", verdict=raw_verdict)
            raw_verdict = det_verdict

        # Downgrade-only: clamp to be no more aggressive than deterministic
        if _VERDICT_ORDER.get(raw_verdict, 1) > _VERDICT_ORDER[det_verdict]:
            logger.warning(
                "LLM tried to upgrade verdict — clamping to deterministic",
                llm_verdict=raw_verdict,
                det_verdict=det_verdict,
            )
            raw_verdict = det_verdict

        llm_verdict = cast(Verdict, raw_verdict)
        logger.info(
            "[stage 1] llm verdict",
            det=det_verdict,
            llm=llm_verdict,
            final=llm_verdict,
            reason=decision.get("reason", ""),
        )
        return llm_verdict, reasoning

    def _build_stage1_prompt(
        self,
        score: float,
        breakdown: dict,
        trigger_reason: str,
        det_verdict: str,
        signal_bundle: SignalBundle,
    ) -> tuple[str, str]:
        system = (
            "You are a risk assessment module for a crypto trading agent.\n"
            "You receive two categories of signals:\n"
            "- FACTUAL: objective, measurable market data (price vs MA, trend, "
            "funding rates, volatility). These are deterministic and verifiable.\n"
            "- SENTIMENT/SUBJECTIVE: fear & greed index, news sentiment, macro "
            "event flags. These reflect crowd psychology and may drive irrational "
            "short-term moves.\n\n"
            "Weigh both categories explicitly in your reasoning. In bull markets "
            "sentiment often leads price; in bear markets facts matter more.\n\n"
            "Respond in this exact format:\n"
            "<reasoning>[your analysis — address facts AND sentiment separately, "
            "then reconcile]</reasoning>\n"
            "<decision>\n"
            "```json\n"
            '{{"verdict": "RISK_ON"|"UNCERTAIN"|"RISK_OFF", "reason": "<20 words"}}\n'
            "```\n"
            "</decision>"
        )

        open_positions = signal_bundle.portfolio.open_positions
        positions_str = (
            ", ".join(p.asset for p in open_positions) if open_positions else "none"
        )

        # --- Factual signals ---
        btc = signal_bundle.prices.get("BTC")
        fact_lines = [
            "FACTUAL SIGNALS (objective, measurable):",
            f"  EMA proximity: {breakdown['ema_signal']:.4f}"
            f" (BTC vs EMA50 1h — 0=far below, 0.5=at MA, 1=far above)",
            f"  BTC 24h trend: {breakdown['btc_trend_norm']:.4f}"
            f" (0=down ≥7%, 0.5=flat, 1=up ≥7%)",
            f"  Funding rate: {breakdown['funding_score']:.4f}"
            f" (inverted-U: 1.0=healthy, <0.5=overcrowded or negative)",
            f"  Volatility regime: {breakdown['volatility_signal']:.4f}"
            f" (0.8=stable, <0.4=extreme expansion/contraction)",
        ]
        if btc is not None:
            fact_lines.append(
                f"  BTC raw: price={btc.price} ema50_1h={btc.ema_50_1h}"
                f" chg_24h={btc.change_24h}% atr_chg_5={btc.atr_chg_5}%"
            )

        # Add per-asset funding context
        funding_details = []
        for asset, onchain in signal_bundle.onchain.items():
            if onchain.funding_rate is not None:
                funding_details.append(f"{asset}={onchain.funding_rate}")
        if funding_details:
            fact_lines.append(f"  Funding rates: {', '.join(funding_details)}")

        # --- Sentiment/subjective signals ---
        fg = signal_bundle.fear_greed
        subj_lines = [
            "",
            "SENTIMENT/SUBJECTIVE SIGNALS (crowd psychology, may be irrational):",
            f"  Fear & Greed: {fg.value}/100 ({fg.classification})"
            f" — normalized={breakdown['fear_greed_norm']:.4f}",
        ]

        # News sentiment per asset
        news_parts = []
        any_macro = False
        for asset, news in signal_bundle.news.items():
            if news.sentiment != 0.0 or news.macro_flag:
                news_parts.append(
                    f"{asset}: sentiment={news.sentiment:.2f}"
                    + (" [MACRO EVENT]" if news.macro_flag else "")
                )
            if news.macro_flag:
                any_macro = True
        if news_parts:
            subj_lines.append(f"  News: {', '.join(news_parts)}")
        else:
            subj_lines.append("  News: no sentiment data available")

        if any_macro:
            subj_lines.append(
                "  ⚠ Macro event active — high uncertainty, "
                f"score penalized ×{self._cfg.brain.stage1.macro_penalty_factor}"
            )

        user = "\n".join([
            f"Market health score: {score:.4f} | Trigger: {trigger_reason}",
            f"Current positions: {positions_str}",
            "",
            *fact_lines,
            *subj_lines,
            "",
            f"Deterministic verdict: {det_verdict}",
            "Rule: you may only output a verdict equal to or more conservative "
            "than the deterministic verdict.",
            "RISK_OFF < UNCERTAIN < RISK_ON (more conservative = lower).",
        ])
        return system, user

    # ------------------------------------------------------------------
    # Stage 2 — Rotation Selector
    # ------------------------------------------------------------------

    async def _stage2(
        self, signal_bundle: SignalBundle, stage1_trace: Stage1Trace
    ) -> tuple[list[SelectedAsset], Stage2Trace]:
        """Stage 2: score all assets, narrow to top-N, LLM re-ranks top N + regime."""
        held_assets = {p.asset for p in signal_bundle.portfolio.open_positions}

        btc_data = signal_bundle.prices.get("BTC")
        btc_change_4h: float | None = None
        if btc_data is not None:
            try:
                btc_change_4h = float(btc_data.change_4h)
            except (ValueError, TypeError):
                pass

        logger.debug(
            "[stage 2] btc reference",
            btc_change_4h=btc_change_4h,
            held_assets=sorted(held_assets),
        )

        scores: dict[str, float] = {
            asset: self._score_asset(asset, data, btc_change_4h, held_assets)
            for asset, data in signal_bundle.prices.items()
        }

        ranked = sorted(scores, key=scores.__getitem__, reverse=True)

        # Cross-run correlation guard: if BTC or ETH is held, exclude both from
        # candidates so the LLM cannot add the correlated pair to the portfolio.
        _BTC_ETH = {"BTC", "ETH"}
        if held_assets & _BTC_ETH:
            excluded = [a for a in ranked if a in _BTC_ETH]
            ranked = [a for a in ranked if a not in _BTC_ETH]
            logger.info(
                "[stage 2] excluded correlated pair from candidates",
                held=sorted(held_assets & _BTC_ETH),
                excluded=excluded,
            )

        logger.info(
            "[stage 2] candidates",
            scores={a: round(scores[a], 4) for a in ranked},
        )
        logger.debug(
            "[stage 2] per-asset details",
            assets={
                asset: {
                    "price": signal_bundle.prices[asset].price,
                    "chg_1h": signal_bundle.prices[asset].change_1h,
                    "chg_4h": signal_bundle.prices[asset].change_4h,
                    "chg_24h": signal_bundle.prices[asset].change_24h,
                    "rsi": signal_bundle.prices[asset].rsi_14_15m,
                    "vol_ratio": signal_bundle.prices[asset].volume_ratio_15m,
                    "atr": signal_bundle.prices[asset].atr_14_15m,
                    "score": round(scores[asset], 4),
                }
                for asset in ranked
            },
        )

        selected, reasoning = await self._call_llm_stage2(
            signal_bundle, ranked, stage1_trace
        )
        pre_filter = [s["asset"] for s in selected]
        selected = _apply_btc_eth_filter(selected)
        if len(selected) != len(pre_filter):
            logger.info(
                "[stage 2] btc/eth correlation filter applied",
                before=pre_filter,
                after=[s["asset"] for s in selected],
            )

        trace = Stage2Trace(
            candidate_scores={a: round(scores[a], 4) for a in scores},
            top_candidates=ranked,
            selected=selected,
            stage2_reasoning=reasoning,
        )
        logger.info(
            "[stage 2] selected",
            selected=selected,
            stage1_verdict=stage1_trace.get("stage1_verdict"),
            uncertainty_multiplier=stage1_trace.get("uncertainty_multiplier"),
        )
        return selected, trace

    def _score_asset(
        self,
        asset: str,
        data: PriceFeedData,
        btc_change_4h: float | None,
        held_assets: set[str],
    ) -> float:
        """Deterministic asset score ≈ [0, 1].

        score = momentum×w.momentum + rel_strength×w.relative_strength + volume×w.volume
        + held_asset_bonus if currently held.
        """
        try:
            change_1h = float(data.change_1h)
            change_4h = float(data.change_4h)
            change_24h = float(data.change_24h)
        except (ValueError, TypeError):
            change_1h = change_4h = change_24h = 0.0

        momentum = (
            0.5 * _norm_change(change_4h, 20.0)
            + 0.3 * _norm_change(change_1h, 20.0)
            + 0.2 * _norm_change(change_24h, 20.0)
        )

        if btc_change_4h is not None:
            rel_strength = _norm_change(change_4h - btc_change_4h, 10.0)
        else:
            rel_strength = 0.5

        try:
            volume_ratio = float(data.volume_ratio_15m)
        except (ValueError, TypeError):
            volume_ratio = 1.0
        volume = _clamp(volume_ratio / 3.0, 0.0, 1.0)

        w = self._cfg.brain.stage2.weights
        score = (
            momentum * w.momentum
            + rel_strength * w.relative_strength
            + volume * w.volume
        )
        if asset in held_assets:
            score += self._cfg.brain.stage2.held_asset_bonus
        return score

    async def _call_llm_stage2(
        self,
        signal_bundle: SignalBundle,
        top_candidates: list[str],
        stage1_trace: Stage1Trace,
    ) -> tuple[list[SelectedAsset], str]:
        """Call LLM to re-rank candidates and assign regimes. Returns (selected, reasoning).

        Raises BrainError on any LLM failure or invalid response — no fallback.
        """
        max_sel = self._cfg.brain.stage2.max_selections
        system, user = self._build_stage2_prompt(signal_bundle, top_candidates, stage1_trace)
        logger.debug("[stage 2] calling LLM", candidates=top_candidates, max_selections=max_sel)
        reasoning, decision = await self._llm.call(system, user)
        logger.debug(
            "[stage 2] LLM response",
            decision=decision,
            reasoning_preview=reasoning[:200] if reasoning else "",
        )

        raw_selections = decision.get("selections", [])
        if not isinstance(raw_selections, list) or not raw_selections:
            logger.error("LLM stage2 returned no valid selections", raw=decision)
            raise BrainError(f"stage2 LLM returned no valid selections: {decision!r}")

        top_set = set(top_candidates)
        selected: list[SelectedAsset] = []
        for item in raw_selections[:max_sel]:
            if not isinstance(item, dict):
                logger.error("LLM stage2 selection is not a dict", item=item)
                raise BrainError(f"stage2 LLM selection is not a dict: {item!r}")
            asset = item.get("asset", "")
            regime = item.get("regime", "")
            if asset not in top_set:
                logger.error("LLM stage2 selected unknown asset", asset=asset)
                raise BrainError(f"stage2 LLM selected asset not in candidates: {asset!r}")
            if regime not in _VALID_REGIMES:
                logger.error("LLM stage2 returned invalid regime", asset=asset, regime=regime)
                raise BrainError(f"stage2 LLM returned invalid regime {regime!r} for {asset!r}")
            selected.append(SelectedAsset(asset=asset, regime=cast(Regime, regime)))

        return selected, reasoning

    def _build_stage2_prompt(
        self,
        signal_bundle: SignalBundle,
        top_candidates: list[str],
        stage1_trace: Stage1Trace,
    ) -> tuple[str, str]:
        max_sel = self._cfg.brain.stage2.max_selections
        system = (
            "You are an asset selection module for a crypto trading agent.\n"
            f"Select 1–{max_sel} assets from the candidates and assign a trading regime.\n"
            "You receive FACTUAL data (price, indicators, on-chain) and SUBJECTIVE "
            "data (sentiment, news) for each asset. Weigh both explicitly:\n"
            "- Factual data tells you what IS happening (trend, momentum, volume)\n"
            "- Subjective data tells you what the CROWD believes (sentiment, fear)\n"
            "- In strong trends, sentiment confirms direction. In choppy markets, "
            "sentiment is noise.\n\n"
            "Valid regimes and what each requires:\n"
            "  STRONG_UPTREND — price firmly above key MAs with sustained momentum.\n"
            "                   Can walk above the upper Bollinger Band for extended\n"
            "                   periods. Requires clear RISK_ON market conditions.\n"
            "  BREAKOUT       — fresh move: ATR expanding, volume surging, price just\n"
            "                   breaking a level. Not applicable when ATR is contracting\n"
            "                   after a prior spike (move already happened).\n"
            "                   Requires clear market conviction.\n"
            "  RANGING        — price oscillating within an established band, ATR flat\n"
            "                   or contracting. Preferred when conviction is low.\n"
            "  WEAK_MIXED     — conflicting signals, no clear structure.\n"
            "                   Default under uncertainty. Minimal directional bet.\n\n"
            "Respond in this exact format:\n"
            "<reasoning>[analyze facts and sentiment separately for each candidate, "
            "then reconcile to pick assets and regimes]</reasoning>\n"
            "<decision>\n"
            "```json\n"
            '{"selections": [{"asset": "SYMBOL", "regime": "REGIME"}, ...]}\n'
            "```\n"
            "</decision>"
        )

        verdict = stage1_trace.get("stage1_verdict", "RISK_ON")
        multiplier = stage1_trace.get("uncertainty_multiplier", 1.0)

        lines = [
            f"Market verdict: {verdict} (uncertainty_multiplier={multiplier})",
        ]
        stage1_reasoning = stage1_trace.get("stage1_reasoning", "")
        if stage1_reasoning:
            lines.append(f"Stage 1 reasoning: {stage1_reasoning}")

        # --- Per-asset factual + subjective data ---
        lines.extend(["", "CANDIDATES (ranked by deterministic score):"])

        for i, asset in enumerate(top_candidates, 1):
            data = signal_bundle.prices.get(asset)
            if data is None:
                lines.append(f"\n{i}. {asset} — no data")
                continue
            try:
                price = float(data.price)
                bb_lower = float(data.bb_lower_15m)
                bb_upper = float(data.bb_upper_15m)
                bb_width = bb_upper - bb_lower
                bb_pos = f"{(price - bb_lower) / bb_width:.2f}" if bb_width > 0 else "N/A"
            except (ValueError, TypeError, ZeroDivisionError):
                bb_pos = "N/A"

            # Factual block
            lines.append(f"\n{i}. {asset}  price={data.price}")
            lines.append(
                "   [FACTUAL] Technical indicators:"
            )
            lines.append(
                f"     15m: rsi={data.rsi_14_15m} ema20={data.ema_20_15m}"
                f" ema50={data.ema_50_15m}"
                f" atr={data.atr_14_15m} atr_chg_5={data.atr_chg_5}%"
                f" vol_ratio={data.volume_ratio_15m}"
                f" bb_pos={bb_pos}"
            )
            lines.append(
                f"     1h:  ema50={data.ema_50_1h} ema200={data.ema_200_1h}"
            )
            lines.append(
                f"     Momentum: chg_5m={data.change_5m}%"
                f" chg_1h={data.change_1h}%"
                f" chg_4h={data.change_4h}%"
                f" chg_24h={data.change_24h}%"
            )

            # On-chain factual data
            onchain = signal_bundle.onchain.get(asset)
            if onchain and onchain.funding_rate is not None:
                oc_parts = [f"funding={onchain.funding_rate}"]
                if onchain.oi_change_pct_24h is not None:
                    oc_parts.append(f"OI_chg_24h={onchain.oi_change_pct_24h}%")
                if onchain.liquidated_usd_15m is not None:
                    oc_parts.append(f"liq_15m=${onchain.liquidated_usd_15m}")
                lines.append(f"     On-chain: {', '.join(oc_parts)}")

            # Subjective block
            news = signal_bundle.news.get(asset)
            subj_parts = []
            if news:
                if news.sentiment != 0.0:
                    subj_parts.append(f"sentiment={news.sentiment:.2f}")
                if news.macro_flag:
                    subj_parts.append("MACRO EVENT ACTIVE")
            fg = signal_bundle.fear_greed
            subj_parts.append(f"F&G={fg.value} ({fg.classification})")
            lines.append(f"   [SUBJECTIVE] {', '.join(subj_parts)}")

        lines.extend([
            "",
            f"Rule: select 1–{max_sel} assets from the list above only.",
        ])
        return system, "\n".join(lines)

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _flat_all_open_positions(
        self, trigger_reason: TriggerReason, reasoning: str
    ) -> list[TradeIntent]:
        """Return a single FLAT_ALL TradeIntent."""
        return [TradeIntent(
            asset=None,
            action="FLAT_ALL",
            size_pct=Decimal("0"),
            strategy="trend_following",
            reasoning=reasoning,
            trigger_reason=trigger_reason,
            confidence=1.0,
        )]

    # ------------------------------------------------------------------
    # Stage 3 — Decision Engine
    # ------------------------------------------------------------------

    def _stage3(
        self,
        signal_bundle: SignalBundle,
        stage1_trace: Stage1Trace,
        selected: list[SelectedAsset],
        stage2_trace: Stage2Trace,
    ) -> tuple[list[TradeIntent], Stage3Trace]:
        """Stage 3: ATR-based stops, Kelly sizing, validation, TradeIntent assembly.

        No LLM call — all math is deterministic. Reasoning assembled from trace data.
        Only produces LONG intents; exits are handled by the stop-loss loop.
        """
        held_assets = {p.asset for p in signal_bundle.portfolio.open_positions}
        uncertainty_mult = stage1_trace.get("uncertainty_multiplier", 1.0)
        trigger_reason: TriggerReason = stage1_trace.get(
            "trigger_reason", signal_bundle.trigger_reason
        )

        cfg = self._cfg.brain.stage3
        tracked = list(self._cfg.assets.tracked)

        logger.info(
            "[stage 3] starting",
            selected=[{"asset": s["asset"], "regime": s["regime"]} for s in selected],
            uncertainty_multiplier=uncertainty_mult,
            half_kelly=cfg.half_kelly_fraction,
            regime_sl_tp={r: {"sl": v.sl_mult, "tp": v.tp_mult}
                          for r, v in cfg.regime_sl_tp.items()},
            min_rr=cfg.min_reward_risk_ratio,
            held_assets=sorted(held_assets),
        )

        intents: list[TradeIntent] = []
        skipped_held: list[str] = []
        skipped_atr_zero: list[str] = []
        skipped_rr: list[str] = []
        skipped_validation: list[str] = []

        for item in selected:
            asset: str = item["asset"]
            regime: Regime = item["regime"]

            # Skip already-held positions
            if asset in held_assets:
                logger.info("[stage 3] skip: already held", asset=asset)
                skipped_held.append(asset)
                continue

            # Get price data
            price_data = signal_bundle.prices.get(asset)
            if price_data is None:
                logger.warning("[stage 3] skip: no price data", asset=asset)
                skipped_atr_zero.append(asset)
                continue

            try:
                entry_price = Decimal(str(price_data.price))
                atr = Decimal(str(price_data.atr_14_15m))
            except Exception:
                logger.warning("[stage 3] skip: invalid price/atr", asset=asset)
                skipped_atr_zero.append(asset)
                continue

            # ATR guard — skip before R:R math so the log is clear
            if atr == Decimal("0"):
                logger.warning("[stage 3] skip: ATR is zero", asset=asset)
                skipped_atr_zero.append(asset)
                continue

            # Stop-loss / take-profit — regime-specific multipliers
            sl_tp = cfg.regime_sl_tp[regime]
            stop_loss = entry_price - atr * Decimal(str(sl_tp.sl_mult))
            take_profit = entry_price + atr * Decimal(str(sl_tp.tp_mult))

            # R:R gate
            risk = entry_price - stop_loss
            if risk <= 0:
                logger.warning(
                    "[stage 3] skip: risk <= 0",
                    asset=asset,
                    entry=str(entry_price),
                    sl=str(stop_loss),
                )
                skipped_rr.append(asset)
                continue
            rr = float((take_profit - entry_price) / risk)

            logger.debug(
                "[stage 3] levels",
                asset=asset,
                entry=str(entry_price),
                atr=str(atr),
                stop_loss=str(stop_loss),
                take_profit=str(take_profit),
                risk=str(risk),
                rr=round(rr, 3),
                min_rr=cfg.min_reward_risk_ratio,
                rr_ok=rr >= cfg.min_reward_risk_ratio,
            )

            if rr < cfg.min_reward_risk_ratio:
                logger.info(
                    "[stage 3] skip: R:R below minimum",
                    asset=asset,
                    rr=round(rr, 3),
                    min_rr=cfg.min_reward_risk_ratio,
                )
                skipped_rr.append(asset)
                continue

            # Position sizing
            rm = cfg.regime_multipliers
            regime_mult = getattr(rm, regime)
            size_pct = (
                Decimal(str(cfg.half_kelly_fraction))
                * Decimal(str(regime_mult))
            )

            strategy = _REGIME_TO_STRATEGY[regime]

            logger.debug(
                "[stage 3] sizing",
                asset=asset,
                regime=regime,
                strategy=strategy,
                half_kelly=cfg.half_kelly_fraction,
                regime_mult=regime_mult,
                final_size_pct=str(size_pct),
            )

            # Confidence: weighted average of signal quality factors
            health_score = stage1_trace.get("market_health_score", 0.5)
            rr_factor = _clamp(rr / cfg.min_reward_risk_ratio, 0.0, 2.0) / 2.0
            confidence = _clamp(
                _CONF_W_HEALTH * health_score
                + _CONF_W_UNCERTAINTY * uncertainty_mult
                + _CONF_W_REGIME * _REGIME_CONFIDENCE.get(regime, 0.5)
                + _CONF_W_RR * rr_factor,
                0.1, 1.0,
            )

            reasoning = _build_stage3_reasoning(
                asset=asset,
                regime=regime,
                entry_price=entry_price,
                stop_loss=stop_loss,
                take_profit=take_profit,
                size_pct=size_pct,
                rr=rr,
                stage1_reasoning=stage1_trace.get("stage1_reasoning", ""),
                stage2_reasoning=stage2_trace.get("stage2_reasoning", ""),
            )

            intent = TradeIntent(
                asset=asset,
                action="LONG",
                size_pct=size_pct,
                stop_loss=stop_loss,
                take_profit=take_profit,
                strategy=strategy,
                reasoning=reasoning,
                trigger_reason=trigger_reason,
                confidence=confidence,
            )

            violations = validate_trade_intent(
                intent,
                tracked_assets=tracked,
                min_rr=cfg.min_reward_risk_ratio,
                max_size=Decimal("1.0"),
                entry_price=entry_price,
            )
            if violations:
                logger.warning(
                    "[stage 3] skip: validation violations",
                    asset=asset,
                    violations=violations,
                )
                skipped_validation.append(asset)
                continue

            logger.info(
                "[stage 3] intent accepted",
                asset=asset,
                action="LONG",
                regime=regime,
                strategy=strategy,
                entry=str(entry_price),
                stop_loss=str(stop_loss),
                take_profit=str(take_profit),
                size_pct=str(size_pct),
                rr=round(rr, 3),
                confidence=confidence,
            )
            intents.append(intent)

        # FLAT/FLAT_ALL before LONG (future-proofing — stage3 only produces LONGs now)
        intents.sort(key=lambda i: _ACTION_ORDER.get(i.action, 99))

        trace = Stage3Trace(
            intents_produced=len(intents),
            skipped_held=skipped_held,
            skipped_atr_zero=skipped_atr_zero,
            skipped_rr=skipped_rr,
            skipped_validation=skipped_validation,
        )
        logger.info(
            "[stage 3] complete",
            intents_produced=len(intents),
            skipped_held=skipped_held,
            skipped_atr_zero=skipped_atr_zero,
            skipped_rr=skipped_rr,
            skipped_validation=skipped_validation,
        )
        return intents, trace