## ADDED Requirements

### Requirement: DeterministicLLMBrain class
The system SHALL provide a `DeterministicLLMBrain` class at `src/agent/brain/deterministic_llm.py` that implements the `Brain` protocol. `run(signal_bundle)` SHALL execute Stage 1 (Market Filter) and return a list of `TradeIntent`. Stages 2 and 3 SHALL raise `NotImplementedError` pending Tasks 16–17.

#### Scenario: Returns list of TradeIntent
- **WHEN** `brain.run(signal_bundle)` is called with a populated SignalBundle
- **THEN** it returns a non-empty list of `TradeIntent` instances (or an empty list if RISK_OFF with no open positions)

### Requirement: Stage 1 deterministic health score
`DeterministicLLMBrain` SHALL compute a `health_score` in `[0.0, 1.0]` from four weighted signals before any LLM call. Weights are read from `config.brain.market_filter.weights` and MUST sum to 1.0.

Signal computations:
- `ema200_filter`: `1.0` if `prices["BTC"].price > prices["BTC"].ema_200`, else `0.0`; `0.5` if BTC is absent
- `fear_greed_norm`: `signal_bundle.fear_greed.value / 100`
- `btc_trend_norm`: `clamp(btc_24h_change_pct / config.brain.market_filter.btc_trend_clamp_pct, -1.0, 1.0)` mapped to `[0, 1]` as `(val + 1.0) / 2.0`; `0.5` if BTC absent
- `funding_score`: per-asset score where `funding_rate ≥ 0 → 1.0`, linearly interpolated to `0.0` at `−0.03%`; average across assets with non-null funding; `0.5` if no data

```
health_score = ema200_filter × w.ema200
             + fear_greed_norm × w.fear_greed
             + btc_trend_norm × w.btc_trend
             + funding_score × w.funding
```

#### Scenario: All bullish signals → high score
- **WHEN** BTC price is above EMA200, fear/greed=80, BTC 24h change=+5%, all funding positive
- **THEN** `health_score` exceeds `0.6`

#### Scenario: All bearish signals → low score
- **WHEN** BTC is below EMA200, fear/greed=20, BTC 24h change=-5%, all funding negative
- **THEN** `health_score` is below `0.4`

#### Scenario: BTC absent → neutral defaults used
- **WHEN** `signal_bundle.prices` does not contain `"BTC"`
- **THEN** `ema200_filter` and `btc_trend_norm` each contribute `0.5` and no exception is raised

#### Scenario: No funding data → neutral default
- **WHEN** all `OnchainData.funding_rate` values are `None`
- **THEN** `funding_score` is `0.5` and the stage completes without error

### Requirement: Stage 1 verdict mapping
`DeterministicLLMBrain` SHALL map `health_score` to a deterministic verdict before calling the LLM:
- `health_score > config.brain.market_filter.risk_on_threshold` → `RISK_ON`
- `health_score < config.brain.market_filter.risk_off_threshold` → `RISK_OFF`
- Otherwise → `UNCERTAIN`

#### Scenario: Score above risk_on threshold maps to RISK_ON
- **WHEN** `health_score = 0.75` and `risk_on_threshold = 0.6`
- **THEN** deterministic verdict is `RISK_ON`

#### Scenario: Score below risk_off threshold maps to RISK_OFF
- **WHEN** `health_score = 0.35` and `risk_off_threshold = 0.4`
- **THEN** deterministic verdict is `RISK_OFF`

#### Scenario: Score in range maps to UNCERTAIN
- **WHEN** `health_score = 0.52`, `risk_off_threshold = 0.4`, `risk_on_threshold = 0.6`
- **THEN** deterministic verdict is `UNCERTAIN`

### Requirement: RISK_OFF short-circuit
When the deterministic verdict is `RISK_OFF`, `DeterministicLLMBrain` SHALL skip the LLM call and immediately return one `FLAT` `TradeIntent` per open position in `signal_bundle.portfolio.positions`. If there are no open positions, it SHALL return an empty list.

#### Scenario: RISK_OFF with open positions → FLAT intents
- **WHEN** verdict is `RISK_OFF` and portfolio has two open positions (SOL, AVAX)
- **THEN** returns exactly two `TradeIntent` with `action="FLAT"`, one per open position, and no LLM call is made

#### Scenario: RISK_OFF with no open positions → empty list
- **WHEN** verdict is `RISK_OFF` and `portfolio.open_position_count == 0`
- **THEN** returns `[]` and no LLM call is made

### Requirement: LLM downgrade-only enforcement
When the deterministic verdict is `RISK_ON` or `UNCERTAIN`, `DeterministicLLMBrain` SHALL call the LLM with the XML+JSON prompt. The LLM response verdict MUST only be accepted if it is equal to or more conservative than the deterministic verdict. If the LLM tries to upgrade, the deterministic verdict SHALL be used and a warning logged.

Verdict conservatism order (most → least conservative): `RISK_OFF < UNCERTAIN < RISK_ON`.

#### Scenario: LLM downgrades RISK_ON to UNCERTAIN → accepted
- **WHEN** deterministic verdict is `RISK_ON` and LLM responds `UNCERTAIN`
- **THEN** final verdict is `UNCERTAIN`

#### Scenario: LLM downgrades RISK_ON to RISK_OFF → accepted
- **WHEN** deterministic verdict is `RISK_ON` and LLM responds `RISK_OFF`
- **THEN** final verdict is `RISK_OFF` and FLAT intents are returned

#### Scenario: LLM tries to upgrade UNCERTAIN to RISK_ON → clamped
- **WHEN** deterministic verdict is `UNCERTAIN` and LLM responds `RISK_ON`
- **THEN** final verdict remains `UNCERTAIN` and a warning is logged

#### Scenario: LLM unavailable → fallback to deterministic verdict
- **WHEN** the LLM client raises after all retries
- **THEN** the deterministic verdict is used, the error is logged, and the brain continues

### Requirement: Stage 1 reasoning extraction
`DeterministicLLMBrain` SHALL extract the `<reasoning>` block from the LLM response and store it as `stage1_reasoning` in an in-progress trace dict. This trace is passed to Stages 2 and 3 and eventually uploaded to IPFS in Stage 3 (Task 17).

#### Scenario: Reasoning block extracted when present
- **WHEN** the LLM response contains `<reasoning>some analysis</reasoning>`
- **THEN** `stage1_reasoning` in the trace is `"some analysis"`

#### Scenario: Reasoning block absent → empty string fallback
- **WHEN** the LLM response contains no `<reasoning>` tag
- **THEN** `stage1_reasoning` is `""` and no exception is raised

### Requirement: UNCERTAIN applies size multiplier
When the final Stage 1 verdict is `UNCERTAIN`, `DeterministicLLMBrain` SHALL set an `uncertainty_multiplier = 0.5` that Stage 3 will apply to position sizes. This multiplier SHALL be passed via the in-progress trace dict.

#### Scenario: UNCERTAIN sets uncertainty_multiplier to 0.5
- **WHEN** final verdict is `UNCERTAIN`
- **THEN** trace dict contains `uncertainty_multiplier = 0.5`

#### Scenario: RISK_ON sets uncertainty_multiplier to 1.0
- **WHEN** final verdict is `RISK_ON`
- **THEN** trace dict contains `uncertainty_multiplier = 1.0`

### Requirement: factory wires deterministic_llm
`make_brain(config)` in `src/agent/brain/factory.py` SHALL return a `DeterministicLLMBrain` instance when `config.brain.implementation == "deterministic_llm"`.

#### Scenario: Factory returns DeterministicLLMBrain
- **WHEN** `make_brain` is called with `config.brain.implementation = "deterministic_llm"`
- **THEN** the returned object is a `DeterministicLLMBrain` and satisfies the `Brain` protocol
