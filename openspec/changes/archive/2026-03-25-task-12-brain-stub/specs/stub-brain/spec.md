## ADDED Requirements

### Requirement: StubBrain implements Brain protocol
`StubBrain` in `src/agent/brain/stub.py` SHALL implement the `Brain` protocol. It SHALL be deterministic and make no LLM calls, no HTTP calls, and no database calls.

#### Scenario: StubBrain is a valid Brain
- **WHEN** `StubBrain` is instantiated with an `AgentConfig`
- **THEN** `isinstance(stub_brain, Brain)` SHALL return True

### Requirement: Stage 1 — deterministic health score
`StubBrain` SHALL compute `health_score` using the formula and weights from config, then map it to a verdict without any LLM call.

Formula:
```
health_score =
  ema200_filter    × config.brain.market_filter.weights.ema200
  fear_greed_norm  × config.brain.market_filter.weights.fear_greed
  btc_trend_norm   × config.brain.market_filter.weights.btc_trend
  funding_score    × config.brain.market_filter.weights.funding
  netflow_score    × config.brain.market_filter.weights.netflow
```

Component derivations (from `signal_bundle`):
- `ema200_filter`: `1.0` if `price > ema_200` for the asset, else `0.0` (uses BTC as reference)
- `fear_greed_norm`: `signal_bundle.fear_greed.value / 100`
- `btc_trend_norm`: BTC `change_24h` normalized: `clamp(change_24h / 10, -1, 1) × 0.5 + 0.5`
- `funding_score`: average funding rate across assets in `signal_bundle.onchain`; `1.0` if rate > 0, `0.5` if 0, `0.0` if negative
- `netflow_score`: `0.5` (neutral stub — netflow data not yet available in OnchainData model)

#### Scenario: Score above risk_on_threshold yields RISK_ON
- **WHEN** computed `health_score > config.brain.market_filter.risk_on_threshold`
- **THEN** Stage 1 verdict SHALL be `RISK_ON` and processing continues to Stage 2

#### Scenario: Score below risk_off_threshold yields RISK_OFF and FLAT intents
- **WHEN** computed `health_score < config.brain.market_filter.risk_off_threshold`
- **THEN** Stage 1 verdict SHALL be `RISK_OFF`
- **THEN** `StubBrain.run()` SHALL return one FLAT `TradeIntent` per currently open position in `signal_bundle.portfolio.open_positions`, with `stop_loss=Decimal("0")`, `take_profit=Decimal("0")`, `size_pct=Decimal("1")`, `reasoning_uri="stub://"`, `trigger_reason` copied from the signal context
- **THEN** processing SHALL NOT continue to Stage 2 or 3

#### Scenario: Score in uncertain range proceeds with halved sizes
- **WHEN** `risk_off_threshold <= health_score <= risk_on_threshold`
- **THEN** Stage 1 verdict SHALL be `UNCERTAIN`
- **THEN** all position sizes computed in Stage 3 SHALL be multiplied by `0.5`

### Requirement: Stage 2 — deterministic asset ranking and regime classification
`StubBrain` SHALL score all assets in `signal_bundle.prices`, apply the held-asset bonus, and select the top 1–2 assets. It SHALL then classify each selected asset's regime using rule-based logic.

Asset scoring formula:
```
asset_score =
  momentum_score    × config.brain.asset_ranker.weights.momentum
  rel_strength      × config.brain.asset_ranker.weights.relative_strength
  volume_confirm    × config.brain.asset_ranker.weights.volume
```

Component derivations (per asset from `signal_bundle.prices[asset]`):
- `momentum_score`: `0.4×change_1h + 0.4×change_4h + 0.2×change_24h`, normalized 0–1 across all assets
- `rel_strength`: `asset_change_24h - btc_change_24h`, normalized 0–1
- `volume_confirm`: `min(volume_ratio, 3.0) / 3.0`
- Held-asset bonus: add `config.brain.asset_ranker.held_asset_bonus` to score if asset has an open position in `signal_bundle.portfolio.open_positions`

Correlation filter: if both top-2 assets are BTC and ETH, drop ETH (keep only BTC).

Regime classification (rule-based, per selected asset):
- `STRONG_UPTREND`: `price > ema_20 > ema_50 > ema_200` AND `50 <= rsi_14 <= 65` AND `volume_ratio > 1.5`
- `BREAKOUT`: `volume_ratio > 2.0` AND `price > bb_upper`
- `RANGING`: `atr_14 < average_atr` (proxy: `rsi_14` between 35–65 and price between `bb_lower` and `bb_upper`)
- `WEAK_MIXED`: anything else

Skip assets with `WEAK_MIXED` regime (no trade intent generated for them).

#### Scenario: Held asset receives scoring bonus
- **WHEN** asset X has an open position in `signal_bundle.portfolio.open_positions`
- **THEN** X's score SHALL have `held_asset_bonus` added before ranking

#### Scenario: BTC+ETH correlation filter
- **WHEN** top-2 ranked assets are BTC and ETH
- **THEN** ETH SHALL be dropped and only BTC selected

#### Scenario: WEAK_MIXED assets are skipped
- **WHEN** an asset is selected but classified as WEAK_MIXED
- **THEN** no TradeIntent SHALL be generated for that asset

### Requirement: Stage 3 — ATR-based sizing and intent assembly
`StubBrain` SHALL compute stop_loss, take_profit, and position size for each selected asset, validate the R:R ratio, and assemble `TradeIntent` objects.

```
stop_loss   = entry_price - (atr_14 × config.brain.atr_stop_multiplier)
take_profit = entry_price + (atr_14 × config.brain.atr_target_multiplier)
size_pct    = config.brain.half_kelly_fraction × regime_multiplier × uncertainty_multiplier
```

Regime multipliers: `STRONG_UPTREND=1.0`, `BREAKOUT=0.75`, `RANGING=0.5`, `WEAK_MIXED=0.25`
Uncertainty multiplier: `0.5` if Stage 1 verdict was `UNCERTAIN`, else `1.0`

Entry price is the current `price` from `signal_bundle.prices[asset]`.

Strategy tag per regime: `STRONG_UPTREND` → `trend_following`, `BREAKOUT` → `breakout`, `RANGING` → `mean_reversion`.

#### Scenario: R:R below minimum skips trade
- **WHEN** `(take_profit - entry_price) / (entry_price - stop_loss) < config.brain.min_reward_risk_ratio`
- **THEN** no TradeIntent SHALL be generated for that asset

#### Scenario: UNCERTAIN verdict halves all sizes
- **WHEN** Stage 1 verdict is `UNCERTAIN`
- **THEN** all `size_pct` values SHALL be multiplied by `0.5` (uncertainty_multiplier)

#### Scenario: Valid trade produces correct TradeIntent fields
- **WHEN** R:R >= min and regime is not WEAK_MIXED
- **THEN** TradeIntent SHALL have `action="LONG"`, correct `stop_loss`, `take_profit`, `size_pct`, `strategy`, `reasoning_uri="stub://"`, and `trigger_reason` from context

### Requirement: FLAT intents sort before LONG intents
The caller (clock loop, etc.) is responsible for sorting, but `StubBrain.run()` SHALL return FLAT intents before LONG intents in the returned list.

#### Scenario: Mixed intent list ordering
- **WHEN** run() produces both FLAT and LONG intents
- **THEN** all FLAT intents SHALL appear before any LONG intents in the returned list
