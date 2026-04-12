# Architecture

## System Diagram

```
+-----------------------------------------------------------------+
|  AGENTS (Go / Ruby / Python)                                     |
|  Each agent = LLM + strategy prompt + tools                      |
|  Platform SDK handles MCP, LLM, persistence, lifecycle          |
+------------------------------------------------------------------+
|  LLM GATEWAY (:8093)           |  MCP GATEWAY (:8095)           |
|  OpenAI-compatible proxy.       |  Single entry point.           |
|  Logs requests, token usage,    |  Every tool call ->            |
|  enables LLM guardrails.        |  policy evaluation.            |
+------------------------------------------------------------------+
|  BACKEND (trading-server:8091)                                   |
+----------+----------+----------+----------+---------------------+
| Trading  | Market   | Files    | Code     | News     |
| MCP      | Data MCP | MCP      | Sandbox  | MCP      |
+----------+----------+----------+----------+---------------------+
|  POLICY ENGINE (Swiftward)                                      |
|  YAML rulesets, entity state, decision traces, hash chain       |
|  v1 (active, enforced) + v2 (shadow, logged only)               |
+------------------------------------------------------------------+
|  OBSERVABILITY                                                   |
|  SigNoz (:3301) — ClickHouse-backed traces, metrics, and logs   |
|  via signoz-otel-collector from trading-server + swiftward      |
+------------------------------------------------------------------+
|  ON-CHAIN (ERC-8004 + Hackathon Infrastructure)                  |
|  Identity Registry, Reputation Registry, Validation Registry    |
+------------------------------------------------------------------+
|  HACKATHON-PROVIDED                                              |
|  Risk Router (enforces limits, routes to DEX)                    |
|  Capital Vault (funded sub-accounts per team)                    |
+-----------------------------------------------------------------+
```

Agents submit EIP-712 signed TradeIntents to the hackathon-provided Risk Router, which enforces on-chain limits and routes trades to a DEX. We do NOT call Uniswap directly.

## Components

### Agents

Each agent is an LLM with a **strategy prompt** (defined by the owner) and access to tools. Every 30 min (or on price alert), the agent wakes up, sees its portfolio and market state, analyzes data using the code sandbox, and decides what to do - open, close, hold, add, reduce positions.

The strategy prompt is free-text (e.g., "Trade BTC and ETH. Be aggressive in trending markets, conservative in ranging." or "Focus on DOGE - find the best entry points"). Everything else - data analysis, hypothesis formation, position sizing, timing - is the agent's autonomous reasoning.

- **Claude Agent Alpha** (Go harness + Claude Code CLI) - autonomous agent using Claude Code as a subprocess via stream-json protocol. 15-min interval. Event-driven loop with Telegram integration. See "Claude Agent Runtime" below.
- **Claude Agent Gamma** (same harness, multi-subagent prompts) - multi-agent debate with bull/bear analysis. 30-min interval.
- **Delta / Epsilon** (Go / Rust) - LLM agents using OpenAI tool-calling. Load a strategy prompt, discover MCP tools at startup, run a tool-calling loop.
- **Random Agent** (Go) - demo agent, random trades, 60s interval.

Agents do NOT know about: policy engine, blockchain, wallet keys, other agents. They see MCP tools via the MCP Gateway.

### LLM Gateway

OpenAI-compatible HTTP proxy on port `:8093` inside `swiftward-server`. Agents point their OpenAI client `base_url` here instead of directly to OpenAI.

**What it does**:
- Proxies `/v1/chat/completions` to the configured upstream (OpenAI, Claude, Ollama, etc.)
- Logs every request: model, messages (full), token usage (`prompt_tokens`, `completion_tokens`, `total_tokens`), duration, `finish_reason`
- Enables optional LLM guardrails (input/output policy evaluation) via the policy engine - same pattern as MCP Gateway
- Exports traces, metrics, and logs via OTEL to SigNoz (signoz-otel-collector → SigNoz ClickHouse)

**Config**: Provider config (OpenAI base URL, API key, models) lives in Swiftward's DB - seeded via `swiftward/seed.sql`. Service-level settings are env vars:
- `SWIFTWARD__LLM_GATEWAY__ENABLED=true`, `SWIFTWARD__LLM_GATEWAY__ADDR=:8093`
- Agent sets `TRADING__LLM_AGENT__LLM_URL=http://swiftward-server:8093/v1`

**Agent transparency**: Agents never know they're behind a gateway. They call a standard OpenAI-compatible endpoint and get standard responses.

### MCP Gateway

Proxies all agent tool calls to backend MCP servers. On every call:

1. Forward event to policy engine (sync gRPC)
2. If approved -> route to backend MCP, return result
3. If rejected -> return verdict + reasons, call blocked

Port `:8095` for agents (API key auth). `fail_open` mode - requests pass through even if rules don't match the event type (Phase 1).

### MCP Servers

5 agent-facing MCP servers + 1 operator API behind the gateway. See [mcps/](../mcps/) for full specs.

| MCP | Purpose | Audience | Status |
|-----|---------|----------|--------|
| Trading | Trade execution (`trade/submit_order`), portfolio, limits, history | Agent | **7/7 tools implemented** (Go) |
| Market Data | Prices, candles, orderbook, funding, OI, alerts | Agent | **Implemented** (Go) |
| Files | Agent workspace filesystem (read/write/edit/append/delete/list/find/search) | Agent | **Implemented** (Go) |
| Code Sandbox | Python execution sandbox (execute + install), persistent container per agent | Agent | **Implemented** (Go) |
| News | Headlines, sentiment, events | Agent | Spec ready |
| Risk | Halt/resume agents, agent status, list agents | Operator (Dashboard UI) | **Implemented** (Go) |

Risk MCP is NOT for agents - it's an operator control panel for the Dashboard UI. Agents learn their constraints from Trading MCP responses (verdicts, portfolio, limits).

### Policy Engine (Swiftward)

Commercial component, included as Docker image. Not open source.

**What it does**:
- Evaluates every trade order against YAML rule policies
- Tracks per-agent entity state (positions, counters, labels, buckets)
- Produces decision traces with full computation evidence
- Hash-chains all decisions (keccak256, tamper-evident)
- Supports two policy versions simultaneously (active + shadow)
- Circuit breakers, velocity limits, concentration limits, drawdown protection
- Agent halt mechanism (atomic, persisted to DB, visible across instances)

**What agents see**: approved/rejected verdicts with reasons. Agents never interact with the policy engine directly.

**Trust model**:
| Tier | What | Where |
|------|------|-------|
| 0 | Fixed per-tx limits (position size, leverage, daily loss) | Risk Router (hackathon-provided, on-chain) |
| 1 | Cross-tx policy + entity state + evidence | Policy Engine (Swiftward, off-chain) |
| 2 | Independent re-execution + validation score | Validator -> ERC-8004 Validation Registry |

### On-Chain (ERC-8004) - All Three Required

The hackathon mission explicitly requires using all three ERC-8004 registries:

- **Identity Registry**: Each agent is an ERC-721 NFT with agentURI metadata. Alpha registered (agentId=1612), wallet linked.
- **Validation Registry**: After each trade, agent posts a validation request with evidence hash. Validator independently verifies and posts response with score (0-100). Provides "clean validation artifacts" (tracks: trustless, validation).
- **Reputation Registry**: Validator posts 6 metrics as numeric feedback via `giveFeedback()`. Self-feedback is blocked by the contract - must come from validator wallet, not agent. Metrics computed from trades table:
  - Performance: `perf/sharpe` (risk-adjusted return), `perf/return_pct` (total return), `perf/win_rate`
  - Risk: `risk/max_drawdown_pct` (worst peak-to-trough)
  - Trust: `trust/compliance_pct` (% of intents that passed policy), `trust/guardrail_saves` (blocked trade count - proof guardrails work)

### Hackathon Infrastructure (provided, not built by us)

- **Risk Router**: On-chain contract enforcing per-tx limits (max position size, max leverage, whitelisted markets, daily loss limit). Receives EIP-712 signed TradeIntents, verifies via ERC-1271 (AgentWallet), routes to DEX.
- **Capital Vault**: Funded sub-accounts per team. Test funds by default, optional real capital for finals.

### Trade Database (PostgreSQL)

Separate from the policy engine DB. PostgreSQL is the **single source of truth** for trade history, portfolio state, and evidence chain. No in-memory caches. Enables horizontal scaling of Trading MCP instances (all read/write the same DB).

**Schema** (single migration: `postgres/migrations/001_init.up.sql`):

| Table | Purpose | Key columns |
|-------|---------|-------------|
| `trades` | Every trade order (approved + rejected) | agent_id, pair, side, qty, price, value, pnl, status, value_after, decision_hash, order_id, tx_hash |
| `agent_state` | Per-agent persistent state | agent_id (PK), cash, initial_value, peak_value, fill_count, reject_count, chain_nonce, halted |
| `decision_traces` | Hash-chained evidence traces | decision_hash (PK), agent_id, prev_hash, trace_json (JSONB), created_at, seq (BIGSERIAL) |

All monetary columns use `NUMERIC(20,8)`. Agent state has `CHECK (cash >= 0)` to prevent overdraw across instances.

**Key design decisions** (full details in `docs/decisions/trade-db.md`):
- **No positions table** - positions computed from trade log (sum buys - sells per market)
- **No portfolio_snapshots table** - portfolio value curve from `value_after` on each trade
- **Delta-based state updates** - `cash = cash + $delta` (not absolute writes), safe under concurrent access
- **Advisory lock** - `pg_advisory_lock` per agent serializes policy + exchange + persist across instances
- **DB-backed evidence chain** - `decision_traces` table with BIGSERIAL `seq` for deterministic ordering
- **DB-backed nonce** - `chain_nonce` atomically incremented, no in-memory fallback
- **DB-backed halt flag** - `halted` column replaces process-local `atomic.Bool`

**Migrations**: Run via `migrate/migrate` Docker image as a compose service. Go code never touches schema.

### Evidence API

Public HTTP endpoint for evidence lookup:

```
GET /v1/evidence/{hash}
```

Returns the full trace JSON for a given decision hash. Returns 404 for unknown hashes, 500 for storage failures (distinguished via `ErrTraceNotFound` sentinel - does not mask DB errors as 404).

Runs on a separate port from the main MCP server.

### Claude Agent Runtime

Go harness (`golang/internal/agents/claude_runtime/`) that orchestrates Claude Code CLI as a child process. Replaces the older OpenAI-based LLM agent for production use.

**Process model**: `claude-agent` binary (PID 1 in container) spawns `claude --output-format stream-json --input-format stream-json` as a child process in a new process group. Communication via NDJSON on stdin/stdout.

**Event-driven main loop** (`loop.go`):
```
main goroutine: select {
    sessionDoneCh    - background session finished
    exec.SessionDoneCh - Claude output <<<SESSION_DONE>>> (start recycle timer)
    intervalTimer    - recycle/restart timer fired
    alertTicker      - poll for triggered alerts (every 30s)
    tgWake           - Telegram message arrived while idle
    ctx.Done()       - shutdown
}
```

Sessions run in a background goroutine. The main loop stays responsive to all events.

**Event table**:

| Event | Session alive? | Action |
|-------|---------------|--------|
| User message | Yes | Inject via stdin (instant) |
| User message | No | Start --continue session immediately |
| SESSION_DONE text | Yes | Start recycle timer (interval duration) |
| Recycle timer | Yes | Kill session, start fresh immediately |
| Interval timer | No | Start fresh session |
| Alert | Yes | Inject as `[SYSTEM ALERT]` message |
| Alert | No | Start fresh session with alert data |
| `/clear` | Yes | Kill session, agent sits idle |
| `/clear` | No | Stop timer, agent sits idle |
| Session exits | - | Schedule interval timer (unless /clear) |

**Telegram integration** (`telegram.go`): Placeholder+edit pattern (send "Analyzing..." message, edit in place as output streams). Supports `/clear` and `/reset` commands. Messages filtered by chat ID, topic ID, and user allowlist.

**Key files**:
- `loop.go` - event-driven main loop, session lifecycle, alert polling
- `executor.go` - Claude CLI process management, stream-json protocol, stdin injection
- `telegram.go` - Telegram bot, output buffering, placeholder+edit pattern
- `procgroup.go` - process group cleanup (SIGTERM/SIGKILL)
- `config.go` - configuration (interval, model, telegram, alert polling)
- `lifecycle.go` - lifecycle event emission to Swiftward

**Compose services** (see `compose.yaml`, profiles `alpha` and `gamma`):
- `agent-alpha-claude` - momentum trader (Swiftward Alpha, agentId=32), 15-minute interval, Sonnet 4.6, prompt directory `prompts/agent-alpha-claude/`
- `agent-gamma-claude` - multi-agent trading system with bull/bear debate (Swiftward Gamma, agentId=43), 30-minute interval, Sonnet 4.6, prompt directory `prompts/agent-gamma-claude/`
- Both share the same `claude-agent` Docker image; interval, model, and prompt directory differ.

## Data Flows

### Trade Order (Happy Path)

```
Agent -> trade/submit_order -> Trading MCP
  -> acquire per-agent advisory lock (pg_advisory_lock)
  -> read agent state + positions from DB
  -> compute portfolio value, position %, drawdown %
  -> send enriched event to Policy Engine via MCP Gateway:
     {order: {pair, side, value}, portfolio: {value, peak, cash}}
  -> Policy Engine evaluates rules against event data + its own entity state
  -> approved -> execute on exchange
  -> compute state deltas (cash, peak, PnL)
  -> sign EIP-712 TradeIntent -> submit to Risk Router (if configured)
  -> compute + persist evidence trace (hash-chained to previous)
  -> INSERT trade + UPDATE agent_state atomically (delta-based)
  -> release advisory lock
  -> return: status, price, qty, decision_hash, prev_hash
```

### Trade Order (Rejected)

```
Agent -> trade/submit_order -> Trading MCP
  -> acquire advisory lock
  -> read agent state from DB
  -> check halt flag -> if halted: reject immediately
  -> send enriched event to Policy Engine
  -> rejected (position_limit, concentration_limit)
  -> INSERT trade (status=reject, no price/value) + UPDATE reject_count
  -> release advisory lock
  -> return: status=reject, reason
```

### Reputation Feedback (Periodic)

```
Trading MCP (using validator key, not agent key)
  -> SELECT trades from DB -> compute metrics (sharpe, drawdown, win_rate, compliance)
  -> call giveFeedback() x6 on Reputation Registry (one tx per metric)
  -> metrics posted on-chain, queryable via getSummary()
```

### Agent Analysis Cycle

```
PRE-SESSION (platform, before LLM wakes up):
  -> market/get_candles(save_to_file=true) for each tracked market+interval
     writes CSVs to trading_data volume at /data/market/*.csv (no LLM context used)
  -> market/get_prices, get_funding, get_open_interest -> injected as compact text tables
  -> files/read memory/MEMORY.md + today/yesterday session logs -> injected via {{memory}} placeholder
  -> portfolio read -> injected as NAV, positions, PnL summary
  All of the above assembled into dynamic context, LLM session starts

LLM SESSION (tool-calling loop):
  -> LLM sees context: portfolio, prices, indicators, memory, and list of pre-downloaded CSV paths
  -> code/execute: reads /data/market/ETH-USDC_1h.csv (already on volume), runs analysis
     sandbox container starts on first code/execute call (~400ms, then warm for 30min)
  -> market/get_candles(save_to_file=true) if more data needed (other market/interval)
     Go writes to /data/workspace/{agent-id}/market/; LLM gets path back, reads in next code/execute
  -> news/search for sentiment data if relevant
  -> trade/submit_order with full reasoning
  -> files/edit: update memory/MEMORY.md with learnings and patterns
  -> files/append: log cycle to memory/sessions/{today}.md
  -> LLM stops tool-calling loop, session ends

AFTER SESSION:
  sandbox container stays running (idle timeout = 30 min)
  /data/workspace/{agent-id}/ preserved across restarts (persistent volume)
  next cycle: container still warm, /workspace/ bind-mount intact, analysis scripts ready
```

**Key design**: Market data never enters LLM context as raw rows. It flows:
`Market Data MCP (Go) -> /data/workspace/{agent-id}/market/ (volume) -> sandbox container (/workspace/market/) -> analysis summary -> LLM context`

**Volume layout**:
```
trading_data volume:
  /data/
    workspace/{agent-id}/     <- Files MCP root (read-write, LLM full control)
      memory/MEMORY.md        <- Core memory (preloaded into LLM context each session)
      memory/sessions/        <- Session logs (YYYY-MM-DD.md, append-only)
      market/                 <- Market Data MCP writes CSVs here (save_to_file=true)
      news/                   <- News MCP exports (future)
```

## Docker Services

From `compose.yaml`. See `docs/architecture/services.md` for the complete list with profiles.

| Service | Image | Port | Role |
|---------|-------|------|------|
| `postgres` | postgres:18-alpine | 5432 (host: 5435) | Shared DB (trading, swiftward, ruby) |
| `redis` | redis:alpine | 6379 (internal) | Cache (news, prices, agent state) |
| `swiftward-server` | `swiftward-server` (GHCR or local) | **5174** (Control UI), **8093** (LLM GW), **8095** (MCP GW), **8097** (Inet GW) | Policy engine + embedded Control UI + MCP/LLM/Inet Gateways |
| `trading-server` | built from `./golang` | **8091, 8092** (exposed) | Trading MCP + Risk MCP + Files MCP + Code MCP + Market Data MCP + News MCP + Evidence API + Dashboard UI |
| `agent-random` | built from `./golang` | - | Random trader demo agent (profile: `random`) |
| `agent-{alpha,gamma}-claude` | `claude-agent` | - | Claude Code agents (profiles: `alpha`, `gamma`) |
| `signoz` + `signoz-otel-collector` + `signoz-clickhouse` + `signoz-zookeeper` | SigNoz | **3301** (UI), 4317/4318 (OTLP) | Observability: traces, metrics, logs |
| `sandbox-python` | `sandbox-python:local` or GHCR | - (managed by Code MCP) | Python REPL container (one per agent, started on first `code/execute` call, idle timeout 30min) |

**No agent is always-on.** Start agents via `make up PROFILES=<csv>` - see `docs/architecture/services.md` for the command reference.

### Exposed Endpoints (from host)

| Endpoint | URL | Auth |
|----------|-----|------|
| LLM Gateway | `http://localhost:8093/v1` | Bearer (agent API key) |
| MCP Gateway (via Swiftward) | `http://localhost:8095/mcp/*` | API key |
| Trading MCP (direct, dev only) | `http://localhost:8091/mcp/trading` | API key |
| Evidence API | `http://localhost:8092/v1/evidence/{hash}` | - |
| Trading Dashboard UI | `http://localhost:8091` | - |
| Swiftward Control UI | `http://localhost:5174` | - |
| SigNoz (traces, metrics, logs) | `http://localhost:3301` | - |
