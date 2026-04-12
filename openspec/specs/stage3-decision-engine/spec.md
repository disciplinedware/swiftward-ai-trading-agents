## ADDED Requirements

### Requirement: ATR-based stop-loss and take-profit computation
Stage 3 SHALL compute `stop_loss = entry_price − (atr_14 × atr_stop_multiplier)` and `take_profit = entry_price + (atr_14 × atr_target_multiplier)` for each selected asset, using `Decimal` arithmetic throughout.

#### Scenario: Normal ATR produces valid levels
- **WHEN** `entry_price=100`, `atr_14=2`, `atr_stop_multiplier=1.5`, `atr_target_multiplier=3.0`
- **THEN** `stop_loss=97.0` and `take_profit=106.0`

#### Scenario: ATR is zero — trade skipped
- **WHEN** `atr_14=0` for an asset
- **THEN** a warning is logged and that asset is skipped (no TradeIntent produced)

### Requirement: Minimum reward-risk ratio gate
Stage 3 SHALL skip an asset if `(take_profit − entry_price) / (entry_price − stop_loss) < min_reward_risk_ratio` (default 2.0).

#### Scenario: R:R below minimum
- **WHEN** computed R:R is 1.8 and `min_reward_risk_ratio=2.0`
- **THEN** the asset is skipped with a log entry; no TradeIntent is produced

#### Scenario: R:R exactly at minimum passes
- **WHEN** computed R:R equals exactly 2.0
- **THEN** the TradeIntent IS produced

### Requirement: Half-Kelly position sizing with multipliers
Stage 3 SHALL compute `size_pct = half_kelly_fraction × regime_multiplier × uncertainty_multiplier` where regime multipliers are: `STRONG_UPTREND=1.0`, `BREAKOUT=0.75`, `RANGING=0.5`, `WEAK_MIXED=0.25`, and `uncertainty_multiplier` comes from Stage 1 trace (0.5 if UNCERTAIN, 1.0 otherwise).

#### Scenario: STRONG_UPTREND with RISK_ON multiplier
- **WHEN** `half_kelly_fraction=0.09`, regime=`STRONG_UPTREND`, `uncertainty_multiplier=1.0`
- **THEN** `size_pct=0.09`

#### Scenario: RANGING with UNCERTAIN multiplier
- **WHEN** `half_kelly_fraction=0.09`, regime=`RANGING`, `uncertainty_multiplier=0.5`
- **THEN** `size_pct=0.0225` (= 0.09 × 0.5 × 0.5)

### Requirement: Already-held asset skip
Stage 3 SHALL skip an asset that already has an open position in the portfolio — no intent to re-enter an existing position.

#### Scenario: Asset already held
- **WHEN** selected asset is present in `signal_bundle.portfolio.open_positions`
- **THEN** the asset is skipped; no LONG TradeIntent is produced for it

### Requirement: TradeIntent validation before submission
A standalone `validate_trade_intent(intent, tracked_assets, min_rr, max_size) -> list[str]` function SHALL return a list of violation strings. An empty list means valid. Stage 3 SHALL skip any intent with violations.

#### Scenario: Valid LONG intent
- **WHEN** all fields are within bounds and R:R ≥ 2.0
- **THEN** `validate_trade_intent` returns `[]`

#### Scenario: stop_loss not below entry_price
- **WHEN** `stop_loss >= entry_price` on a LONG intent
- **THEN** `validate_trade_intent` returns a non-empty list containing a descriptive violation

#### Scenario: Asset not in tracked list
- **WHEN** intent asset is not in `tracked_assets`
- **THEN** `validate_trade_intent` returns a non-empty list

### Requirement: Regime-to-strategy-tag mapping
Stage 3 SHALL map regime to `strategy` as follows: `STRONG_UPTREND` → `"trend_following"`, `BREAKOUT` → `"breakout"`, `RANGING` / `WEAK_MIXED` → `"mean_reversion"`.

#### Scenario: Each regime maps correctly
- **WHEN** regime is one of `STRONG_UPTREND`, `BREAKOUT`, `RANGING`, `WEAK_MIXED`
- **THEN** the TradeIntent `strategy` matches the mapping table above

### Requirement: Programmatic reasoning assembly
Stage 3 SHALL assemble `TradeIntent.reasoning` from Stage 1 reasoning text + Stage 2 reasoning text + key Stage 3 math fields (entry_price, stop_loss, take_profit, size_pct, R:R). No LLM call is made in Stage 3.

#### Scenario: Reasoning includes all key fields
- **WHEN** Stage 3 produces a LONG intent
- **THEN** the `reasoning` string contains the asset name, stop_loss, take_profit, size_pct, and R:R value

### Requirement: FLAT-before-LONG ordering
Stage 3 SHALL return intents sorted so `FLAT` and `FLAT_ALL` actions come before `LONG` actions. (Stage 3 currently only produces LONGs, but the sort SHALL be applied unconditionally for future-proofing.)

#### Scenario: Mixed action list is ordered correctly
- **WHEN** the intent list contains both FLAT and LONG intents
- **THEN** all FLAT intents appear before all LONG intents in the returned list
