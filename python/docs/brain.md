# How the Trading Brain Works

This is the decision engine behind the agent. It runs every 15 minutes (or on special triggers like price spikes), takes in market data, and outputs concrete trade instructions: what to buy, at what size, with exact stop-loss and take-profit levels.

The brain has three stages, executed in sequence. Each stage acts as a filter — if an earlier stage says "no", the later stages never run.

```
Market Data ──→ [Stage 1: Should we trade at all?]
                        │
                   RISK_OFF → close everything, done
                   RISK_ON / UNCERTAIN ↓
                                       │
                [Stage 2: Which coins to trade?]
                        │
                   nothing selected → done
                   1-2 assets selected ↓
                                       │
                [Stage 3: How exactly to trade them?]
                        │
                   TradeIntents (LONG SOL @ $140, SL $136, TP $150, size 4.5%)
```

---

## Stage 1 — Market Filter

**Question: "Is the crypto market healthy enough to take new positions?"**

This is a market-level check, not about any specific coin. It computes a health score from 0.0 (maximum bearish) to 1.0 (maximum bullish) by combining 5 weighted signals, then classifies:

- **> 0.6** → `RISK_ON` — green light, proceed to Stage 2
- **0.4 – 0.6** → `UNCERTAIN` — proceed but with reduced position sizes (halved)
- **< 0.4** → `RISK_OFF` — red light, close all open positions immediately

### The 5 signals

#### 1. EMA Proximity (weight: 30%)

How far is BTC's price from its 50-period EMA on the 1h chart. Uses a sigmoid curve so the score is continuous — not a binary "above/below" flip.

```
BTC 3% above EMA50 1h  →  ~0.99 (very bullish)
BTC 1% above           →  ~0.82
BTC at EMA             →   0.50 (neutral)
BTC 1% below           →  ~0.18
BTC 3% below           →  ~0.01 (very bearish)
```

Why EMA50 1h and not EMA200? We're trading 15m candles. EMA200 on 1h is an ~8-day lookback — too slow. EMA50 1h (~2 days) catches trend shifts while filtering out noise.

#### 2. BTC 24h Trend (weight: 25%)

BTC's 24-hour price change, clamped to ±7% and normalized to [0, 1]. This captures the dominant market direction on a daily scale.

```
BTC +7% or more  →  1.0
BTC +3.5%        →  0.75
BTC flat          →  0.50
BTC -3.5%        →  0.25
BTC -7% or worse →  0.0
```

The 7% clamp means we don't over-react to extreme crash days — once BTC is down 7%, additional downside doesn't make the score more bearish.

#### 3. Funding Rate (weight: 20%)

Aggregated across all tracked assets. Uses an **inverted-U** shape because both extremes are bad:

- **Zero funding** = neutral (no directional conviction) → 0.5
- **Slightly positive (~0.01%)** = healthy bullish bias, longs paying a small premium → 1.0 (peak)
- **Very positive (>0.03%)** = overcrowded longs, liquidation risk → declines toward 0.3
- **Negative** = shorts dominating → linearly declines to 0.0 at -0.03%

```
Funding Rate    Score    What it means
──────────────────────────────────────────────────
  +0.05%        0.30     Danger: longs way overcrowded
  +0.03%       ~0.50     Elevated: getting crowded
  +0.01%        1.00     Healthy bullish bias
   0.00%        0.50     Neutral
  -0.015%      ~0.50     Shorts building up
  -0.03%        0.00     Bearish: shorts dominant
```

#### 4. Fear & Greed Index (weight: 15%)

Uses an S-curve (logistic function) instead of linear normalization, so extreme readings matter much more than mid-range noise:

```
F&G Index    Linear    S-Curve    Difference
─────────────────────────────────────────────
   10         0.10      0.04      Amplified fear
   30         0.30      0.17      More bearish
   50         0.50      0.50      Same at midpoint
   70         0.70      0.83      More bullish
   90         0.90      0.96      Amplified greed
```

This index updates daily, so it gets a lower weight (15%). It's useful for catching sentiment extremes but too slow for 15m trading.

#### 5. Volatility Regime (weight: 10%)

Based on BTC's ATR (Average True Range) change over 5 periods on the 15m chart. Measures whether volatility is expanding, stable, or contracting.

```
ATR change    Score    Market condition
──────────────────────────────────────────────────
  +60%        ~0.25    Volatility exploding (flash crash territory)
  +20%        ~0.70    Expanding (be cautious)
   ±10%        0.80    Stable (ideal for trading)
  -20%        ~0.70    Compressing (pre-breakout)
  -80%        ~0.40    Dead market (no opportunity)
```

Stable ATR is best for trading. Rapidly expanding ATR means stops get hit more often. Extremely compressed ATR means there's nothing to trade.

### Macro Event Penalty

After computing the weighted score, if any tracked asset has an active macro flag (FOMC, CPI, major regulatory events), the score gets multiplied by 0.85. This can push a borderline RISK_ON (0.65) down to UNCERTAIN (0.55), adding caution during events that create binary risk no technical signal can model.

### LLM Safety Check

After the deterministic score, an LLM reviews the numbers. It can only **downgrade** the verdict (RISK_ON → UNCERTAIN, or UNCERTAIN → RISK_OFF), never upgrade. If the LLM is down, the deterministic verdict stands.

This means the deterministic math sets the ceiling, and the LLM can add caution but never add aggression.

---

## Stage 2 — Rotation Selector

**Question: "Which specific coins should we trade right now?"**

### Step 1: Score every asset (deterministic)

Each of the 10 tracked assets gets a score based on:

| Factor | Weight | What it measures |
|---|---|---|
| Momentum | 40% | Weighted mix of 1h (30%), 4h (50%), 24h (20%) price changes |
| Relative Strength | 35% | Outperformance vs BTC over 4h — are we picking a leader or a laggard? |
| Volume | 25% | Current volume vs average (volume ratio on 15m). Higher = more conviction |

Held assets get a +5% bonus to avoid unnecessary churn (selling a position just to re-buy it).

### Step 2: Filter correlated pairs

If BTC or ETH is already in the portfolio, both BTC and ETH are excluded from candidates. They're too correlated — holding both doubles risk without doubling edge.

### Step 3: LLM picks 1-2 assets + assigns regime

All candidates (ranked by score) go to the LLM with full technical data: price, RSI, EMAs, ATR, ATR change, Bollinger Band position, volume ratio.

The LLM must:
1. Pick 1-2 assets from the candidate list
2. Assign each a **regime** that determines the trading strategy:

| Regime | Strategy | Position Size | When to use |
|---|---|---|---|
| STRONG_UPTREND | trend_following | 100% of base | Price firmly above MAs, sustained momentum |
| BREAKOUT | breakout | 75% | Fresh move: ATR expanding, volume surging |
| RANGING | mean_reversion | 50% | Sideways channel, ATR flat/contracting |
| WEAK_MIXED | mean_reversion | 25% | Conflicting signals, unclear structure |

If both BTC and ETH are selected, ETH gets dropped (correlation filter).

Unlike Stage 1, there's **no fallback** if the LLM fails in Stage 2 — the cycle aborts. This is intentional: asset selection and regime assignment require judgment that can't be reduced to a formula.

---

## Stage 3 — Decision Engine

**Question: "At what price, stop-loss, take-profit, and size should we enter?"**

This is pure math, no LLM. For each selected asset:

### Stop-Loss and Take-Profit (ATR-based)

Uses the 14-period ATR on 15m candles, with regime-specific multipliers:

| Regime | SL = entry - ATR × | TP = entry + ATR × | R:R |
|---|---|---|---|
| STRONG_UPTREND | 2.0 | 4.0 | 2.0 |
| BREAKOUT | 1.5 | 4.5 | 3.0 |
| RANGING | 1.5 | 3.0 | 2.0 |
| WEAK_MIXED | 1.5 | 3.0 | 2.0 |

**Example**: SOL at $140, ATR = $2, regime STRONG_UPTREND:
- Stop-loss: $140 - $2 × 2.0 = **$136**
- Take-profit: $140 + $2 × 4.0 = **$148**
- R:R = $8 / $4 = **2.0**

### Position Sizing (Half-Kelly)

```
size = half_kelly × regime_multiplier × uncertainty_multiplier
```

With default half_kelly = 9%:

| Regime | × RISK_ON (1.0) | × UNCERTAIN (0.5) |
|---|---|---|
| STRONG_UPTREND (×1.0) | 9.0% | 4.5% |
| BREAKOUT (×0.75) | 6.75% | 3.375% |
| RANGING (×0.50) | 4.5% | 2.25% |
| WEAK_MIXED (×0.25) | 2.25% | 1.125% |

### Safety Gates

Before a trade intent is emitted, it must pass:

1. **Not already held** — skip if we already have a position in this asset
2. **ATR > 0** — can't calculate stops without volatility data
3. **R:R >= 2.0** — reward must be at least 2× the risk
4. **Validation** — asset must be in tracked list, size must be within bounds, stop < entry, TP > entry

Any failed check = skip that asset, log why, move on.

### Confidence Score

Each intent gets a confidence score (0.1 to 1.0) used downstream for execution priority:

```
confidence = 0.3 × health_score      (Stage 1 market quality)
           + 0.2 × uncertainty_mult  (1.0 if RISK_ON, 0.5 if UNCERTAIN)
           + 0.3 × regime_confidence (1.0 for STRONG_UPTREND ... 0.4 for WEAK_MIXED)
           + 0.2 × rr_factor         (how much R:R exceeds the minimum)
```

---

## What happens after Stage 3

The brain outputs a list of `TradeIntent` objects. These go to the trading MCP server which:
1. Checks risk limits (max concurrent positions, drawdown caps)
2. Routes through SwiftWard (policy engine) for approval
3. Executes on the exchange (or paper-trades)

The brain itself never touches the exchange. It only produces intents — the execution layer decides if and how to fill them.

---

## Trigger Types

The brain doesn't run continuously. It's triggered by:

| Trigger | Interval | What happens |
|---|---|---|
| `clock` | Every 15 min | Full 3-stage cycle — the main loop |
| `price_spike` | 1 min check | Fires if any tracked asset moves ≥3% in 1 min |
| `stop_loss` | 2 min check | Monitors open positions against their stops |
| `fear_greed` | On threshold | Fires when F&G drops <20 or rises >80 |

All triggers feed the same 3-stage pipeline. The `trigger_reason` is passed through for logging and reasoning, but doesn't change the math.
