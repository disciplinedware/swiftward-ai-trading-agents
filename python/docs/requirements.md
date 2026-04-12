# AI Crypto Trading Agent — Requirements

Hackathon: [AI Trading Agents ERC-8004](https://lablab.ai/ai-hackathons/ai-trading-agents-erc-8004/)
Dates: March 15–29, 2026
Stack: Python / asyncio / FastAPI
Repo: Monorepo

---

## 1. Overview

A long-only spot crypto trading agent optimized for PnL. The agent tracks 10 assets, runs a 15-minute base loop, and uses event-driven triggers for faster reaction. All reasoning is anchored on-chain via ERC-8004 registries. Trade execution goes through a hackathon-provided Risk Router contract via a Uniswap-style DEX router on an EVM-compatible chain.

The agent is LLM-agnostic: the brain is designed to work with any OpenAI-compatible API (local Ollama models, GPT-4o, Claude via proxy). The active model is configured via YAML config.

All configuration — structure, tunable values, and secrets — lives in a single `config/config.yaml` which is gitignored. A `config/config.example.yaml` with placeholder values is committed to the repo.

---

## 2. Architecture and Runtime Flow

This section is the primary reference for understanding how the agent works end-to-end. Read this before any other section.

---

### 2.1 Component Overview

The system consists of four distinct runtime layers:

**Agent process** (`src/agent/`) — the central orchestrator. Runs all trigger loops, executes the brain pipeline, and sends TradeIntents. It never reads from or writes to a database directly. All data access goes through MCP servers.

**MCP servers** (`src/mcp_servers/`) — five independent FastAPI services. Each exposes a single MCP JSON-RPC interface used by both the LLM agent (via tool calls) and the lightweight polling loops (via direct HTTP POST). All external API calls, database access, and on-chain interactions are encapsulated here. The agent never calls external APIs directly.

**SwiftWard** (teammate's service) — a transparent HTTP proxy that sits between the agent and `trading-mcp`. The agent points its trading URL at SwiftWard. SwiftWard intercepts `POST /execute` calls, applies Risk Guard rules, and forwards approved or modified intents to `trading-mcp`. All other routes pass through untouched. From the agent's perspective it is invisible — just a URL in config.

**Backtesting** (`src/backtesting/`) — a self-contained runner that replaces all MCP servers with stubs backed by historical CSV data. The agent brain runs unchanged. Only the data sources are swapped.

---

### 2.2 Data Flow Diagram

```
                    ┌─────────────────────────────────────┐
                    │           EXTERNAL APIS              │
                    │  Binance · Coinglass · Alternative.me│
                    │  CryptoPanic · CryptoQuant · web3    │
                    └──────────────┬──────────────────────┘
                                   │
              ┌────────────────────▼────────────────────────┐
              │              MCP INPUT SERVERS               │
              │  price_feed · news · onchain_data · fear_greed│
              └──────┬─────────────────────────┬────────────┘
                     │ MCP JSON-RPC             │ MCP JSON-RPC
                     │ (polling loops)          │ (brain tool calls)
          ┌──────────▼──────────────────────────▼──────────┐
          │                 AGENT PROCESS                    │
          │                                                  │
          │  ┌─────────────┐    ┌──────────────────────┐   │
          │  │ Trigger Layer│    │       Brain           │   │
          │  │             │    │                       │   │
          │  │ clock 15min │    │ Stage 1: Market Filter│   │
          │  │ price spike │───▶│ Stage 2: Rotation     │   │
          │  │ stop-loss   │    │ Stage 3: Decision     │   │
          │  │ tier2       │    │                       │   │
          │  └─────────────┘    └──────────┬────────────┘   │
          │                               │ TradeIntent      │
          └───────────────────────────────┼─────────────────┘
                                          │
                                          ▼
                                    ┌──────────┐
                                    │ SwiftWard │  (Risk Guard proxy)
                                    └─────┬────┘
                                          │ approved / modified / rejected
                                          ▼
                    ┌─────────────────────────────────────────┐
                    │              trading-mcp                  │
                    │                                           │
                    │  ┌──────────────┐  ┌─────────────────┐  │
                    │  │    Engine    │  │    Portfolio     │  │
                    │  │ paper | live │  │  state (Postgres)│  │
                    │  └──────┬───────┘  └─────────────────┘  │
                    │         │ async post-trade hooks          │
                    │  ┌──────▼───────────────────────────┐   │
                    │  │           ERC-8004                 │   │
                    │  │ identity · validation · reputation │   │
                    │  └──────────────┬───────────────────┘   │
                    └─────────────────┼───────────────────────┘
                                      │
                                      ▼
                            ┌──────────────────┐
                            │  IPFS + on-chain  │
                            │  registries       │
                            └──────────────────┘
```

---

### 2.3 Startup Sequence

When the agent process starts:

1. Load and validate `config/config.yaml`
2. Initialize logger (format and level from config)
3. Connect to all MCP servers — verify each is reachable with a health check
4. Load portfolio state from `trading-mcp GET /portfolio` — if open positions exist, record them in memory for the stop-loss loop
5. Check `trading-mcp` for existing `agentId` — if none, trigger ERC-8004 identity registration
6. Start all four async loops concurrently:
   - Clock loop (15 min)
   - Price spike loop (1 min)
   - Stop-loss / take-profit loop (2 min)
   - Tier 2 loop (5 min)
7. Agent is now running — loops fire independently, all share the same cooldown gate instance

---

### 2.4 Full Trade Cycle — Step by Step

This describes the complete path from a trigger firing to ERC-8004 being updated.

**Step 1 — Trigger fires**

One of four loops detects a condition: clock tick, price spike ±3–5%, stop-loss breach, or Tier 2 event. The stop-loss loop bypasses the cooldown gate and goes directly to Step 6 (exit). All other triggers check the cooldown gate first.

**Step 2 — Cooldown gate check**

Per-asset check: has a trade been made on this asset in the last 30 minutes? If yes → suppress trigger, do nothing. Also checks global position count: if already at max 2 concurrent positions and the intent would be a new LONG → suppress. Stop-loss exits always bypass this gate.

**Step 3 — Signal collection (Brain Stage 1 input)**

The brain fetches fresh portfolio state then calls all MCP input servers in parallel:
- `trading-mcp` → `get_portfolio` — current positions, open count, balances (fetched fresh on every brain run, not cached)
- `price_feed` → OHLCV, indicators (RSI, EMA, ATR, BB, volume ratio) for all 10 assets
- `fear_greed` → current index value (cached, fetched once per day)
- `onchain_data` → funding rates, open interest, liquidation volume, BTC/ETH netflow
- `news` → per-asset sentiment scores + macro flag (batched LLM call inside news-mcp, cached 5 min)

**Step 4 — Brain Stage 1: Market Filter**

Compute deterministic health score from weighted signals. Pass score + raw signals + trigger reason to LLM. LLM can only downgrade the verdict, never upgrade. If verdict is `RISK_OFF` → emit FLAT intent and jump to Step 5b. If `RISK_ON` or `UNCERTAIN` → proceed to Stage 2.

**Step 5a — Brain Stage 2: Rotation Selector**

Score all 10 assets by momentum, relative strength, and volume confirmation. Select top 1–2. Apply correlation filter (no BTC + ETH at full size simultaneously). Run rule-based regime classifier on each selected asset → assign strategy profile (trend-following, breakout, mean-reversion, or weak/mixed). LLM resolves only if regime is ambiguous.

**Step 5b — Brain Stage 3: Decision Engine**

Calculate entry price, stop-loss (1.5× ATR below entry), take-profit (3.0× ATR above entry). Verify minimum 2.0 reward/risk ratio — skip trade if not met. Calculate position size using half-Kelly fraction × regime multiplier × uncertainty multiplier. LLM writes reasoning trace JSON. Reasoning trace uploaded to IPFS → URI returned. TradeIntent assembled with all fields including `reasoning_uri`.

**Step 6 — SwiftWard**

Agent sends `POST /execute` with TradeIntent to SwiftWard URL (configured in `config.yaml`). SwiftWard applies Risk Guard rules and returns one of:
- `approved` → intent passes through to `trading-mcp` unchanged
- `modified` → intent passes through with adjusted size or other fields
- `rejected` → agent logs the reason, includes it in a final reasoning trace update, no trade executed — cycle ends here

**Step 7 — Trade execution (trading-mcp)**

`trading-mcp` receives the approved or modified intent. Routes to paper engine or live engine based on `config.yaml trading.mode`. Paper engine fills at current market price + 0.1% slippage, creates a fake `tx_hash`. Live engine constructs swap calldata, submits to Risk Router contract via web3.py, waits for confirmation (60s timeout). Portfolio state in Postgres is updated atomically with the new position including entry price, stop-loss, and take-profit levels.

**Step 8 — Post-trade sync (agent)**

`trading-mcp` returns execution result to agent. Agent calls `cooldown_gate.record_trade(asset)` → gate closes for this asset for 30 minutes.

**Step 9 — ERC-8004 hooks (trading-mcp, async)**

Non-blocking tasks fire inside `trading-mcp` after execution confirms:
- Validation Registry: reasoning trace hash + IPFS URI submitted on-chain
- Reputation Registry: fires only when a position closes (take-profit hit, stop-loss triggered, or FLAT intent). Realized PnL converted to 0–100 score and posted with strategy tag, asset, and exit reason as tags.

---

### 2.5 Component Dependency Map

Who calls whom. This is the complete import and HTTP dependency graph.

```
agent/loops
  ├── reads from → price_feed-mcp  (price spike check, stop-loss price fetch)
  ├── reads from → onchain_data-mcp (liquidation check)
  ├── reads from → news-mcp         (macro flag, high-impact news)
  ├── reads from → fear_greed-mcp   (threshold crossing)
  ├── reads from → trading-mcp      (open positions for stop-loss loop)
  └── writes to  → trading-mcp      (stop-loss / take-profit exits)

agent/brain
  ├── reads from → price_feed-mcp   (indicators, OHLCV for all assets)
  ├── reads from → fear_greed-mcp   (index value)
  ├── reads from → onchain_data-mcp (funding, OI, liquidations, netflow)
  ├── reads from → news-mcp         (sentiment scores, macro flag)
  ├── reads from → trading-mcp      (get_portfolio — fresh positions at start of each brain run)
  └── writes to  → trading-mcp via SwiftWard (TradeIntent)

trading-mcp/engine
  ├── reads from → price_feed-mcp   (current price for paper fill)
  ├── writes to  → Postgres          (position state)
  └── calls      → erc8004           (post-trade hooks, async)

trading-mcp/erc8004
  ├── writes to  → IPFS              (reasoning trace upload)
  └── writes to  → ERC-8004 on-chain registries (identity, validation, reputation)

backtesting/runner
  ├── replaces   → price_feed-mcp   with price_feed_stub (CSV replay)
  ├── replaces   → news-mcp         with news_stub
  ├── replaces   → trading-mcp      with trading_stub
  └── uses       → agent/brain      unchanged (same code path)
```

**Key rules enforced by this dependency map:**
- `agent/` never calls external APIs directly — always via MCP servers
- `agent/` never touches the database — only `trading-mcp` owns Postgres
- All MCP calls use the same JSON-RPC interface — polling loops and LLM tool calls are identical in transport
- ERC-8004 hooks are internal to `trading-mcp` — `agent/` never calls registries directly
- `backtesting/` reuses the real brain code without modification — only stubs change

---

## 3. Constraints

- **Execution**: Spot only. Long positions and FLAT (stablecoin) only. No shorting, no leverage, no lending protocols.
- **Chain**: EVM-compatible (specific chain TBD — use chain ID from `config.yaml`). All contract addresses injected via config.
- **Assets**: 10 tracked assets. Starting set: BTC, ETH, SOL, BNB, AVAX + 5 TBD (finalized during implementation with Claude Code based on DEX liquidity on target chain).
- **Capital**: Starting balance TBD — set in `config.yaml` under `trading.starting_balance_usdc`.
- **Stablecoin**: USDC or USDT (configured in `config.yaml`). FLAT position = 100% stablecoin.
- **Concurrent positions**: Max 2 at any time.
- **Cooldown**: 30 minutes per asset after any trade on that asset.
- **On restart**: Resume position monitoring immediately from persisted state.

---

## 4. Monorepo Structure

```
python/                              # agent root inside monorepo
├── CLAUDE.md
├── pyproject.toml
├── config/
│   ├── config.example.yaml          # committed — template with placeholder values
│   └── config.yaml                  # gitignored — actual values
│
└── src/
    ├── agent/                       # main agent process
    │   ├── loops/                   # clock, price spike, stop-loss, tier2
    │   ├── trigger/                 # cooldown gate
    │   ├── brain/                   # market filter, rotation selector, decision engine
    │   └── models/                  # TradeIntent, Position, SignalBundle
    │
    ├── mcp_servers/
    │   ├── price_feed/              # OHLCV, indicators — Binance REST
    │   ├── news/                    # headlines, sentiment scoring
    │   ├── onchain_data/            # funding rates, OI, liquidations, netflow
    │   ├── fear_greed/              # Alternative.me index, cached daily
    │   └── trading/                 # execution + portfolio state + ERC-8004 hooks
    │       ├── engine/              # paper and live execution
    │       ├── portfolio/           # position tracking, PnL, persistence
    │       ├── erc8004/             # identity, validation, reputation, ipfs
    │       └── routes/              # /execute, /portfolio, /positions, /pnl
    │
    └── backtesting/                 # runner, MCP stubs, metrics, data download
```

---

## 5. Configuration

A `config/` folder at `python/` root contains two files:

- `config/config.example.yaml` — committed, all keys present with placeholder values
- `config/config.yaml` — gitignored, actual values filled in by each developer

`config.py` loads `config/config.yaml` at runtime using `pyyaml`. No secret substitution — all values including secrets go directly into `config.yaml`.

### config/config.example.yaml

```yaml
chain:
  chain_id: 1                          # TBD — confirm with hackathon
  rpc_url: https://...                 # SECRET
  risk_router_address: "0x..."

assets:
  tracked:
    - BTC
    - ETH
    - SOL
    - BNB
    - AVAX
    # 5 more TBD based on DEX liquidity on target chain
  stablecoin: USDC

llm:
  base_url: http://localhost:11434/v1  # local Ollama default
  model: llama3.2
  api_key: ollama                      # SECRET — replace for GPT-4o / Claude
  max_tokens: 1000
  retries: 3

trading:
  mode: paper                          # paper | live
  starting_balance_usdc: 10000        # TBD
  cooldown_minutes: 30
  max_concurrent_positions: 2
  database_url: postgresql+asyncpg://user:password@localhost:5432/trading_agent

brain:
  half_kelly_fraction: 0.09
  atr_stop_multiplier: 1.5
  atr_target_multiplier: 3.0
  min_reward_risk_ratio: 2.0
  market_filter:
    risk_on_threshold: 0.6
    risk_off_threshold: 0.4
    weights:
      ema200: 0.30
      fear_greed: 0.20
      btc_trend: 0.20
      funding: 0.15
      netflow: 0.15
  asset_ranker:
    weights:
      momentum: 0.40
      relative_strength: 0.35
      volume: 0.25
    held_asset_bonus: 0.05
  price_spike_threshold_pct: 3.0

mcp_servers:
  price_feed_url: http://localhost:8001
  news_url: http://localhost:8002
  onchain_data_url: http://localhost:8003
  fear_greed_url: http://localhost:8004
  trading_url: http://localhost:8006   # SwiftWard URL — set to :8005 to bypass

erc8004:
  identity_registry_address: "0x8004A169FB4a3325136EB29fA0ceB6D2e539a432"
  reputation_registry_address: "0x8004BAa17C55a88189AE136b182e5fdA19dE9b63"
  validation_registry_address: "0x..."
  agent_wallet_private_key: "0x..."   # SECRET
  ipfs_provider: pinata                # pinata | web3storage | mock
  ipfs_api_key: "..."                  # SECRET

external_apis:
  binance_api_key: ""                  # SECRET — optional, higher rate limits
  coinglass_api_key: "..."             # SECRET
  news_api_key: "..."                  # SECRET — TBD provider

logging:
  level: INFO
  format: console                      # console (plain text) | json
```

### .gitignore additions

```gitignore
config/config.yaml
```

---

## 6. Trigger Layer

### 5.1 Clock trigger
- Fires every 15 minutes unconditionally
- Checks cooldown gate before running full agent loop
- If gate is closed → skip tick, sleep until next

### 5.2 Price spike loop
- Polls `price-feed-mcp /prices/change` every 60 seconds
- Fires agent loop if any asset moves ±`config.brain.price_spike_threshold_pct`% in 1-min or 5-min window
- Checks per-asset cooldown gate before firing
- Direct HTTP call to MCP server — no LLM

### 5.3 Stop-loss / take-profit loop
- Polls `trading-mcp /positions` every 2 minutes
- Polls `price-feed-mcp /prices/latest` for each open position
- If price ≤ stop_loss OR price ≥ take_profit → fires immediate exit
- **Bypasses cooldown gate** — always fires
- No LLM call — deterministic price comparison only

### 5.4 Tier 2 loop
- Fires every 5 minutes
- Checks: liquidation volume spike (Coinglass), high-impact news flag (news-mcp)
- Fear & Greed threshold crossing (< 20 or > 80) — read from cached value, not re-fetched
- If condition met → fires agent loop (Stage 1 only for F&G, full loop for liquidations/news)
- Checks per-asset cooldown gate before firing

### 5.5 Cooldown gate
- Per-asset timestamp dict, protected by `asyncio.Lock`
- `record_trade(asset: str)` — called after any confirmed execution (LONG or FLAT)
- `is_open(asset: str) -> bool` — returns True if cooldown has expired
- Stop-loss loop bypasses this entirely
- Global position count check: `portfolio.open_position_count() < MAX_CONCURRENT_POSITIONS`

---

## 7. MCP Input Servers

All MCP servers expose a single interface: the MCP protocol over JSON-RPC (`POST /mcp`). Both the LLM agent (via tool calls) and the lightweight polling loops use this same interface. Polling loops call it directly as a plain HTTP POST with a JSON-RPC payload — no special client needed.

### 7.1 price-feed-mcp
**Data source**: Binance REST API (no auth required for public endpoints)

Tools:
- `get_prices_latest` → current price per asset
- `get_prices_change` → % change over 1m, 5m, 1h, 4h, 24h windows per asset
- `get_indicators` → RSI, EMA, ATR, Bollinger Bands, volume ratio per asset and interval

Signals computed:
- RSI 14 on 15-min candles
- EMA 20, EMA 50 on 15-min candles
- EMA 200 on 1-hour candles (macro trend filter)
- ATR 14 on 15-min candles
- Bollinger Bands 20-period on 15-min candles
- Volume ratio: current candle volume / 20-period average

### 7.2 news-mcp
**Data source**: TBD (CryptoPanic or Newsdata.io — provider abstracted behind interface)

Tools:
- `get_headlines` → recent headlines per asset
- `get_sentiment` → per-asset sentiment score -1.0 to +1.0, batched LLM scoring, cached 5 min
- `get_macro_flag` → boolean flag + reason if macro event detected (Fed, ETF, hack, exchange collapse)

LLM sentiment scoring: all 10 assets batched in one prompt, output strict JSON.

### 7.3 onchain-data-mcp
**Data sources**: Binance Futures API (funding, OI), Coinglass (liquidations), CryptoQuant or on-chain RPC (netflow)

Tools:
- `get_funding_rate` → current funding rate per asset from Binance Futures
- `get_open_interest` → OI value and % change per asset
- `get_liquidations` → total USD liquidated per asset in last 15-min window (Coinglass)
- `get_netflow` → BTC/ETH exchange netflow (inflow = selling pressure, outflow = accumulation)

### 7.4 fear-greed-mcp
**Data source**: Alternative.me API

Tools:
- `get_index` → current Fear & Greed value (0–100), classification, timestamp — fetched once per day, cached in memory
- `get_historical` → historical values for backtesting

### 7.5 trading-mcp
**Dual mode**: paper (default) or live — configured via `config.yaml` under `trading.mode`

Tools:
- `execute_swap` → accepts a single TradeIntent, routes to paper or live engine, returns status, tx_hash, executed price, slippage, and reason if rejected.
- `get_portfolio` → full state: balance, positions, realized PnL, drawdown, open position count
- `get_positions` → open positions with entry price, stop_loss, take_profit, unrealized PnL
- `get_position` → single asset position by symbol
- `get_daily_pnl` → realized PnL for today

**Paper engine**:
- Fills at current market price + 0.1% slippage assumption
- Returns fake `tx_hash` prefixed `paper_`

**Live engine**:
- Constructs calldata for hackathon Risk Router contract
- Submits via web3.py
- Waits for tx confirmation (timeout: 60s)

**Portfolio persistence**:
- PostgreSQL, async SQLAlchemy
- All positions, trades, and PnL persisted to DB
- On agent restart: load from DB → resume monitoring immediately

---

## 8. Brain / LLM Core

### 8.0 Brain interface

All brain implementations conform to a single async protocol defined in `src/agent/brain/base.py`:

```python
class Brain(Protocol):
    async def run(self, signal_bundle: SignalBundle) -> list[TradeIntent]: ...
```

The clock loop and all other trigger loops call `brain.run(signal_bundle)` without knowing which implementation is active. The active implementation is selected at startup by `make_brain(config)` in `src/agent/brain/factory.py` based on `config.brain.implementation`.

Available implementations:

| Value | File | Description |
|-------|------|-------------|
| `stub` | `brain/stub.py` | Deterministic, no LLM. Used in backtesting and initial wiring. |
| `deterministic_llm` | `brain/deterministic_llm.py` | Deterministic pipeline + LLM as auditor (Stage 1 verdict, Stage 2 re-rank, Stage 3 trace). |

Additional implementations can be added by creating a new file in `src/agent/brain/`, implementing the `Brain` protocol, and registering it in `factory.py`.

Config:
```yaml
brain:
  implementation: deterministic_llm   # stub | deterministic_llm | ...
```

### 7.1 LLM client
- OpenAI-compatible client (uses `openai` Python SDK with configurable `base_url`)
- Switch between local Ollama, GPT-4o, Claude via `config.yaml`
- Response format: XML envelope wrapping JSON — `<reasoning>[analysis]</reasoning><decision>```json{...}```</decision>`. More reliable than bare JSON mode across local and multilingual models (GLM, Qwen, Ollama)
- JSON extraction uses a 4-level fallback: `<decision>` tag → text before `[` → text before `{` → full response
- Character encoding normalization applied before `json.loads()`: Chinese quotes (`""`), brackets (`［］｛｝`), and punctuation (`：，`) replaced with ASCII equivalents — required for GLM-4, Qwen, Kimi outputs
- Retry logic: 3 attempts with exponential backoff on timeout
- Max tokens: 1000 per call

### 7.2 Stage 1 — Market Filter

**Deterministic score** (computed before LLM call):
```
health_score =
  ema200_filter    × 0.30   (1.0 if price > EMA200, 0.0 if below)
  fear_greed_norm  × 0.20   (fear_greed_value / 100)
  btc_trend_norm   × 0.20   (normalized BTC 24h return, clamped -1 to 1)
  funding_score    × 0.15   (1.0 if rate > 0, scaled down if negative)
  netflow_score    × 0.15   (1.0 if outflow, 0.0 if strong inflow)
```

**Verdict thresholds**:
- score > 0.6 → `RISK_ON`
- score 0.4–0.6 → `UNCERTAIN` (proceed but all sizes × 0.5)
- score < 0.4 → `RISK_OFF` → emit FLAT intent, skip Stage 2

**LLM role**: receives score + raw signals + trigger reason. Can output `RISK_ON`, `UNCERTAIN`, or `RISK_OFF`. Can only downgrade — never upgrade vs deterministic score. If score is `RISK_OFF`, LLM output is ignored.

**LLM prompt structure**:
```
System: You are a risk assessment module for a crypto trading agent.
        Respond in this exact format:
        <reasoning>[your analysis, free-form]</reasoning>
        <decision>
        ```json
        {"verdict": "RISK_ON"|"UNCERTAIN"|"RISK_OFF", "reason": "<20 words"}
        ```
        </decision>
User:   Market health score: {score}
        Signals: {signal_breakdown}
        Trigger: {trigger_reason}
        Current positions: {positions}
        Rule: you may only output a verdict equal to or more conservative than the score implies.
```

The `<reasoning>` block is extracted and appended to the Stage 3 reasoning trace for ERC-8004. The `<decision>` JSON is parsed for the verdict.

### 7.3 Stage 2 — Rotation Selector

**Asset ranker** (deterministic):
```
asset_score =
  momentum_score    × 0.40   (normalized: 0.4×change_1h + 0.4×change_4h + 0.2×change_24h)
  rel_strength      × 0.35   (asset return - BTC return, same windows)
  volume_confirm    × 0.25   (volume_ratio clamped 0–3, normalized)
```

- Rank all 10 assets descending by score
- Already-held asset bonus: +0.05 to score (uses positions from `get_portfolio` fetched in Step 3)
- Correlation filter: if both top assets are BTC and ETH, pick only BTC (or split 70/30 max)

**Regime classifier** (rule-based):
```
STRONG_UPTREND:   price > EMA20 > EMA50 > EMA200 AND RSI 50–65 AND volume_ratio > 1.5
BREAKOUT:         BB width contracting last 5 candles → current candle expansion + volume_ratio > 2.0
RANGING:          price oscillating, ATR below 20-period ATR average, RSI 35–65
WEAK_MIXED:       anything else
```

Strategy profile per regime:
- `STRONG_UPTREND` → trend-following, full size
- `BREAKOUT` → breakout entry, 75% size
- `RANGING` → mean-reversion, 50% size
- `WEAK_MIXED` → 25% size or skip

**LLM role in Stage 2**: the deterministic ranker narrows the field to top ~4 candidates (eliminates clear losers). The LLM then receives full multi-timeframe context (OHLCV + all indicators across 15m/1h/4h) for those candidates and re-ranks to the final 1–2 selections, assigning a regime to each. This gives the LLM authority over asset selection — where adaptability matters most — while keeping position sizing and risk math deterministic in Stage 3.

LLM prompt format (same XML+JSON envelope as Stage 1):
```
System: You are an asset rotation selector for a crypto trading agent.
        Respond in this exact format:
        <reasoning>[your analysis comparing candidates]</reasoning>
        <decision>
        ```json
        [{"asset": "SOL", "regime": "STRONG_UPTREND", "rank": 1},
         {"asset": "AVAX", "regime": "BREAKOUT", "rank": 2}]
        ```
        </decision>
User:   Verdict from Stage 1: {verdict}
        Candidate assets (top 4 by deterministic score):
        {per_asset_multiframe_context}
        Held positions: {positions}
        Select the best 1–2 assets and assign a regime to each.
```

### 7.4 Stage 3 — Decision Engine

**Position sizing**:
```
base_size    = HALF_KELLY_FRACTION (default 0.09 = 9% of portfolio)
final_size   = base_size × regime_multiplier × uncertainty_multiplier
             where uncertainty_multiplier = 0.5 if Stage 1 verdict was UNCERTAIN
```

Regime multipliers: `STRONG_UPTREND=1.0`, `BREAKOUT=0.75`, `RANGING=0.5`, `WEAK_MIXED=0.25`

**Stop-loss and take-profit** (ATR-based):
```
stop_loss   = entry_price − (ATR14 × ATR_STOP_MULTIPLIER)    default 1.5
take_profit = entry_price + (ATR14 × ATR_TARGET_MULTIPLIER)  default 3.0
```
Minimum reward/risk ratio enforced: 2.0. If ATR-based target gives < 2.0 R:R, skip trade.

**Submission ordering**: when a cycle produces multiple intents (e.g. FLAT one asset, LONG another), the agent sorts them before sequential submission — FLAT/close intents first, LONG opens second. This ensures position slots are freed before new ones are filled. Trading-mcp receives one intent at a time and has no knowledge of ordering.

**TradeIntent validation**: before each intent is submitted to SwiftWard, a `validate_trade_intent(intent) -> list[str]` function runs synchronously and returns a list of violations. An empty list = valid. Non-empty = trade skipped with violations logged. Checks: R:R ≥ 2.0, size_pct within bounds, stop_loss < entry_price, take_profit > entry_price, asset in tracked list, action is LONG or FLAT.

**Reasoning trace** (assembled from all stage outputs, uploaded to IPFS):
```json
{
  "timestamp": "ISO8601",
  "trigger_reason": "price_spike",
  "asset": "SOL",
  "market_health_score": 0.74,
  "signal_breakdown": {},
  "stage1_reasoning": "<extracted from Stage 1 <reasoning> block>",
  "stage1_verdict": "RISK_ON",
  "asset_rank": 1,
  "stage2_reasoning": "<extracted from Stage 2 <reasoning> block>",
  "regime": "STRONG_UPTREND",
  "strategy": "trend_following",
  "entry_price": 143.21,
  "stop_loss": 142.30,
  "take_profit": 156.80,
  "size_pct": 0.09,
  "reward_risk_ratio": 2.15,
  "validation_violations": [],
  "tx_hash": "0x..."
}
```

The reasoning trace is built incrementally: Stage 1 appends its `<reasoning>` text, Stage 2 appends its `<reasoning>` text, Stage 3 fills in the math fields. The completed trace is uploaded to IPFS after `tx_hash` is received.

---

## 9. TradeIntent Model

```python
@dataclass
class TradeIntent:
    asset: str              # e.g. "SOL"
    action: str             # "LONG" | "FLAT"
    size_pct: float         # fraction of portfolio, e.g. 0.09
    stop_loss: float        # price level
    take_profit: float      # price level
    strategy: str       # "trend_following" | "breakout" | "mean_reversion"
    reasoning_uri: str      # IPFS URI of reasoning trace
    trigger_reason: str     # "clock" | "price_spike" | "stop_loss" | "news" | etc.
```

---

## 10. SwiftWard Integration

SwiftWard is a transparent proxy developed by a teammate. It sits between the agent and `trading-mcp`:

```
agent → SwiftWard → trading-mcp
```

From the agent's perspective SwiftWard is invisible — the agent simply points its trading URL at SwiftWard instead of directly at `trading-mcp`. No special client, no extra code.

Configured in `config.yaml`:

```yaml
mcp_servers:
  trading_url: http://localhost:8006   # SwiftWard URL in production
                                       # http://localhost:8005 to bypass (dev/testing)
```

SwiftWard intercepts `execute_swap` tool calls, applies Risk Guard rules (approve / modify / reject), then forwards approved or modified intents to `trading-mcp`. All other tool calls (`get_portfolio`, `get_positions`, `get_daily_pnl`) pass through untouched.

The agent handles all three outcomes via the standard response from `/execute`:
- `approved` / `modified` → trade confirmed, proceed normally
- `rejected` → log reason, include in ERC-8004 reasoning trace, no trade executed

---

## 11. ERC-8004 Trust Layer

All registry interactions use `web3.py`. Contract ABIs stored in `src/mcp_servers/trading/erc8004/abis/`.

### 11.1 Identity Registry — once on first run of trading-mcp
- On first startup, upload an agent registration file to IPFS containing: name, description, version, tracked assets, strategy type, wallet address, and loop interval
- Call `identityRegistry.register(agentURI, agentHash)` with the IPFS URI and its hash
- Persist the returned `agentId` to the database — reused in all subsequent registry calls
- Skip registration on subsequent restarts if `agentId` already exists in DB

### 11.2 Validation Registry — every trade open (async, non-blocking)
- After trade execution is confirmed, attach the `tx_hash` to the reasoning trace
- Upload the full reasoning trace JSON to IPFS, obtain URI and content hash
- Call `validationRegistry.validationRequest()` with the agent wallet as self-validator, the `agentId`, the IPFS URI, and the content hash

### 11.3 Reputation Registry — every trade close (async, non-blocking)
- Triggered when a position closes (take-profit, stop-loss, or manual FLAT)
- Convert realized PnL percentage to a 0–100 score: 50 = breakeven, scaled linearly
- Call `reputationRegistry.giveFeedback()` with the `agentId`, score, tags (strategy tag, asset, exit reason), and the IPFS validation URI from the original trade open

### 11.4 IPFS provider
- Abstracted behind a single upload interface that returns a URI
- Provider selected via `config.yaml` under `erc8004.ipfs_provider`
- Supported: `pinata`, `web3storage` (TBD — implement whichever has a free tier)
- Fallback for local dev: write to local temp directory, return a `mock://` URI

---

## 12. Post-Trade Flow

After `trading-mcp` confirms a trade, the following happens in order:

1. Portfolio state updated synchronously inside `trading-mcp` — position written to Postgres
2. Cooldown gate records the trade for the asset — gate closes for 30 minutes
3. Validation Registry hook fires asynchronously and non-blocking — does not delay trade confirmation
4. When a position closes (take-profit, stop-loss, or FLAT intent): Reputation Registry hook fires asynchronously and non-blocking

ERC-8004 calls are always non-blocking. Trade confirmation is returned to the agent before registry calls complete.

---

## 13. Postgres Schema

Owned entirely by `trading-mcp`. No other component reads from or writes to this database directly.

### agents
Stores the ERC-8004 identity for the agent. Only one row expected in production.

| Column | Type | Notes |
|---|---|---|
| id | serial PK | |
| agent_id | integer | returned by `identityRegistry.register()` |
| wallet_address | text | agent's signing wallet |
| registration_uri | text | IPFS URI of registration file |
| registered_at | timestamptz | |

### positions
One row per open or closed position.

| Column | Type | Notes |
|---|---|---|
| id | serial PK | |
| asset | text | e.g. `SOL` |
| status | text | `open` \| `closed` |
| action | text | `LONG` \| `FLAT` |
| entry_price | numeric | |
| size_usd | numeric | USD value at entry |
| size_pct | numeric | fraction of portfolio at entry |
| stop_loss | numeric | price level |
| take_profit | numeric | price level |
| strategy | text | `trend_following` \| `breakout` \| `mean_reversion` |
| trigger_reason | text | `clock` \| `price_spike` \| `news` \| etc. |
| reasoning_uri | text | IPFS URI of reasoning trace |
| validation_uri | text | IPFS URI after Validation Registry submission |
| opened_at | timestamptz | |
| closed_at | timestamptz | null if still open |
| exit_reason | text | `take_profit` \| `stop_loss` \| `flat_intent` \| null |
| exit_price | numeric | null if still open |
| realized_pnl_usd | numeric | null if still open |
| realized_pnl_pct | numeric | null if still open |
| tx_hash_open | text | on-chain tx hash for entry |
| tx_hash_close | text | on-chain tx hash for exit |

### trades
Append-only log of every execution. Positions reference the most recent trade. Useful for audit and backtesting replay.

| Column | Type | Notes |
|---|---|---|
| id | serial PK | |
| position_id | integer FK → positions.id | |
| direction | text | `open` \| `close` |
| asset | text | |
| price | numeric | executed fill price |
| size_usd | numeric | |
| slippage_pct | numeric | |
| tx_hash | text | |
| executed_at | timestamptz | |

### portfolio_snapshots
Point-in-time portfolio state. Written after every trade execution. Used for PnL charting and drawdown calculation.

| Column | Type | Notes |
|---|---|---|
| id | serial PK | |
| total_usd | numeric | total portfolio value at snapshot time |
| stablecoin_balance | numeric | |
| open_position_count | integer | |
| realized_pnl_today | numeric | resets at UTC midnight |
| peak_total_usd | numeric | rolling peak for drawdown calculation |
| current_drawdown_pct | numeric | (peak - current) / peak |
| snapshotted_at | timestamptz | |

---

## 14. Backtesting

### 14.1 MCP stubs

Three stub MCP servers replace the real ones during backtesting. Each exposes the same MCP JSON-RPC interface as its real counterpart — the agent brain runs completely unchanged.

**price_feed stub** — replays Binance historical klines from CSV files in `backtesting/data/`. Supports time advancement so the runner can step through candles. Computes all the same indicators as the real server.

**news stub** — returns neutral sentiment score (0.0) for all assets unless a historical headlines file is provided. Macro flag is always false unless configured otherwise.

**trading stub** — paper engine only, no real execution. Tracks a virtual portfolio and outputs a full PnL log at the end of the run.

### 14.2 Brain stub

A deterministic rule-based implementation of all three brain stages. No LLM calls — used for fast parameter sweeps and initial strategy validation. Takes the same signal inputs and outputs the same TradeIntent format as the real brain.

### 14.3 Backtest runner

Event-driven: replays one 15-min candle at a time in sequence. Fires trigger conditions deterministically based on price data (spike detection, stop-loss breach). Outputs a trade log and metrics JSON file at the end.

### 14.4 Metrics

Computed at the end of each backtest run:
- Total return (%)
- Sharpe ratio (annualized)
- Max drawdown (%)
- Win rate (%)
- Avg win / avg loss ratio
- Time in market (%)
- Return breakdown by asset
- Return breakdown by strategy tag

### 14.5 Data download

Downloads Binance historical klines for all tracked assets. Default: 12 months, 15-min interval. Saves as CSV per asset in `backtesting/data/`.

---

## 15. Logging and Observability

- Structured logging via Python `logging`, output to stdout only
- Log level configured via `config.yaml` under `logging.level` (default: `INFO`)
- Format toggled via `config.yaml` under `logging.format`:
  - `console` — human-readable plain text for local development
  - `json` — structured JSON for VPS / log aggregation
- Every trigger fire logged with: trigger type, asset, timestamp
- Every brain decision logged with: stage, verdict, score, reasoning summary
- Every trade logged with: asset, action, size, entry price, stop, target, tx hash
- Every ERC-8004 call logged with: registry, outcome, IPFS URI

---

## 16. Implementation Order (recommended for Claude Code)

### Phase 1 — Foundation
Everything else depends on this. No trading logic yet, just the skeleton that all components share.

1. **Project scaffolding** — `pyproject.toml`, `config/` folder, `config.example.yaml`, logger setup (console/json toggle), base exception types
2. **Shared models** — `TradeIntent`, `Position`, `SignalBundle` dataclasses. These are imported by agent, MCP servers, and backtesting — define them first and never change their interfaces lightly

---

### Phase 2 — Data Layer
Build data in before building logic that consumes it.

3. **price_feed MCP server** — Binance OHLCV, price change windows, all indicators (RSI, EMA, ATR, BB, volume ratio). The most-used server — everything depends on it
4. **fear_greed MCP server** — simple, no dependencies, daily cache. Good warmup before the complex servers
5. **onchain_data MCP server** — funding rates, OI, liquidations (Coinglass), netflow
6. **news MCP server** — headlines provider (pick CryptoPanic or Newsdata.io), batched LLM sentiment scoring, macro flag, 5-min cache

---

### Phase 3 — Trading MCP Server
The most complex server. Build it in layers, each layer testable independently.

7. **Postgres schema + migrations** — create all four tables (`agents`, `positions`, `trades`, `portfolio_snapshots`)
8. **Portfolio state** — read/write positions to DB, PnL calculation, drawdown tracking, all `get_*` tools
9. **Paper engine** — `execute_swap` tool, fills at market price + slippage, writes to portfolio state
10. **ERC-8004 + IPFS** — identity registration, validation hook, reputation hook, IPFS provider abstraction (mock first, real provider second)
11. **Live engine** — Risk Router contract calldata, web3.py submission, tx confirmation. Implement last — paper engine covers all testing before this

---

### Phase 4 — Agent Brain
Build deterministic first, add LLM second. This order lets you test strategy logic cheaply before paying for LLM calls.

12. **Brain stub** — deterministic rule-based implementation of all three stages. Same inputs/outputs as real brain. Used in backtesting and initial wiring
13. **Cooldown gate** — per-asset async lock, global position count check
14. **Clock loop + agent entrypoint** — startup sequence, health checks, loop orchestration. Wire brain stub + all MCP servers together. First time the full pipeline runs end-to-end
15. **Real brain: Market Filter (Stage 1)** — deterministic health score + LLM downgrade verdict
16. **Real brain: Rotation Selector (Stage 2)** — asset ranker + regime classifier, LLM for ambiguous regimes only
17. **Real brain: Decision Engine (Stage 3)** — ATR sizing, Kelly fraction, reasoning trace generation, IPFS upload

---

### Phase 5 — Trigger Layer
Add event-driven triggers only after the full clock-based loop is working and tested.

18. **Stop-loss / take-profit loop** — highest priority trigger, bypasses cooldown gate, no LLM
19. **Price spike loop** — 1-min polling, fires agent loop on ±3–5% move
20. **Tier 2 loop** — 5-min polling, liquidation cascade + news flag + Fear & Greed threshold

---

### Phase 6 — Backtesting
Reuses everything built so far. Only adds stubs and a runner.

21. **Historical data download** — Binance klines CSV for all tracked assets, 12 months, 15-min interval
22. **MCP stubs** — price_feed stub (CSV replay + time advancement), news stub, trading stub
23. **Backtest runner + metrics** — event-driven candle replay, Sharpe, drawdown, win rate, breakdown by asset and strategy tag

---

### Phase 7 — Production Wiring
Final integrations before live trading.

24. **SwiftWard integration** — point `trading_url` at SwiftWard, handle approved / modified / rejected responses gracefully

---

## 17. Open Decisions (resolve during implementation)

| Decision | Notes |
|---|---|
| EVM chain | Confirm with hackathon organizers. Set in `config.yaml`. |
| 5 additional tracked assets | Choose based on DEX liquidity on target chain |
| News data provider | CryptoPanic or Newsdata.io — compare free tier limits |
| IPFS provider | Pinata or web3.storage — compare free tier |
| Starting capital | Confirm with hackathon organizers |
| Ollama model | Start with `llama3.2`, upgrade to GPT-4o or Claude if needed |
