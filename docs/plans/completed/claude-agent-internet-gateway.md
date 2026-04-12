# Jailed Claude Code Agent + Three Gateways

> **Status**: ✅ Shipped
> **Packages**: `golang/internal/agents/claude_runtime/`, `golang/cmd/claude-agent/`, `swiftward-server`
> **Compose**: `compose.yaml` (profiles: `alpha`, `gamma`)
> **Network**: `agent-isolated` - outbound only through the Internet Gateway
> **On-chain**: `Swiftward Alpha` (agentId=32), `Swiftward Gamma` (agentId=43)

## What was built

Full Claude Code CLI (v2.1.87) runs inside a hardened Alpine + Node container. A Go harness (`claude-agent` binary) drives its lifecycle via the stream-json protocol. **All network egress from the agent is mediated**: the container literally cannot reach the internet, call an LLM, or invoke an MCP tool without hitting a Swiftward policy gate. Two trading agents ship today - `agent-alpha-claude` (momentum trader) and `agent-gamma-claude` (multi-agent analysis with bull/bear debate).

## The three gateways

### Internet Gateway (`:8097`)

HTTP/HTTPS forward proxy. Every container on `agent-isolated` sets `HTTP_PROXY` and `HTTPS_PROXY` to `http://swiftward-server:8097`. The gateway applies policy on the Host header.

**Blocks**: exfiltration domains (pastebin, transfer.sh, ngrok, webhook.site, `.onion`, MCP proxy), with Telegram alert on block.
**Allows**: Anthropic APIs, market data APIs (Binance, Bybit, Kraken), news APIs (CryptoPanic, CryptoCompare), Telegram API, and internal services (`swiftward-server`, `trading-server`, `postgres`).

**Policy stream**: `inet`
**Key file**: `swiftward/policies/rulesets/inet/v1/rules.yaml`

### LLM Gateway (`:8093`)

OpenAI-compatible proxy. Agents point `ANTHROPIC_BASE_URL` at `http://swiftward-server:8093/agent-<id>` for per-agent isolation. The gateway runs ML injection detection (Llama Guard 2 + BERT classifiers in `docker/classifiers/`) on requests before forwarding to Anthropic. It enforces end-of-session attestation, logs every request (model, messages, tokens, duration), and per-agent rate limits.

**Blocks**: prompt-injection attempts detected by PG2 / BERT, out-of-session LLM calls.
**Allows**: normal Claude API traffic (Opus 3, Sonnet 4.6, Haiku 3.5) after classifiers pass.

**Policy stream**: `llm`
**Key files**: `swiftward/policies/rulesets/llm/v1/prompt-injection.yaml`, `swiftward/policies/rulesets/llm/v1/attention-recovery.yaml`, `docker/classifiers/Dockerfile`

### MCP Gateway (`:8095`)

JSON-RPC 2.0 proxy for all tool calls. Claude hits MCP tools through URLs in `.mcp.json` that point at `http://swiftward-server:8095/mcp/<name>`. The gateway applies per-agent tool permissions, enriches parameters (notably `X-Agent-ID`), and scans tool responses for injection before handing them back to Claude.

**Blocks**: unauthorized tools per agent, injection payloads in news/market responses, malformed parameters.
**Allows**: `trade/*`, `market/*`, `news/*`, `polymarket/*`, `code/*`, `files/*` per policy.

**Policy stream**: `mcp`
**Key files**: `swiftward/policies/rulesets/mcp/v1/rules.yaml`, `swiftward/policies/rulesets/mcp/v1/evals/*.json`

## Container isolation

**Docker image**: `docker/claude-agent/Dockerfile` - two-stage build.

- Stage 1: Go binary build (`golang:1.25-alpine`)
- Stage 2: runtime on `node:24-alpine` with Python 3, git, bash, curl, jq, ripgrep, and Claude Code v2.1.87 pinned

**Network**: `agent-isolated` (internal). The container can only reach `swiftward-server`, `trading-server`, `postgres`, `redis`, `signoz-otel-collector`. All other egress is blocked at the Docker network level.

**Environment**:
- `HTTP_PROXY` / `HTTPS_PROXY` = `http://swiftward-server:8097`
- `ANTHROPIC_BASE_URL` = `http://swiftward-server:8093/agent-<id>`
- `CLAUDE_AGENT__TRADING_MCP_URL` = `http://swiftward-server:8095/mcp/trading`
- `CLAUDE_AGENT__MARKET_MCP_URL` = `http://swiftward-server:8095/mcp/market`
- ...and one URL per MCP

**Credentials**: injected at container start via `docker/claude-agent/entrypoint.sh`. Priority order: macOS Keychain extract → `${HOME}/.claude` mounted read-only → `ANTHROPIC_API_KEY` env var. Each agent gets a writable copy at `/home/app/.claude` so per-agent memory and settings stay isolated.

**Volumes**:
- `/home/app/.claude` - Claude Code settings and auto-memory (persistent across restarts)
- `/workspace` - agent workspace (shared with Files and Code Sandbox MCPs)
- `/workspace/.claude` - read-only `CLAUDE.md` + agent config mounted from `prompts/agent-<id>/`

## Go harness (`golang/internal/agents/claude_runtime/`)

**State machine** (full detail: `docs/architecture/claude-runtime-state-machine.md`):

| State | Process | Claude | Interval timer |
|-------|---------|--------|----------------|
| **Working** | alive | generating or executing tool calls | stopped |
| **Idle** | alive, stdin open | finished, waiting for input | running |
| **Killed** | not running | - | may be running |

**Key rule: never close stdin during a session.** Closing stdin kills Claude immediately, breaking injection into Idle state. Termination is always via context cancel → SIGTERM to the process group.

**Transitions**:
- `TurnDoneCh` fires on Claude's `result/success` → Working transitions to Idle, timer starts
- Interval timer fires → kill (SIGTERM), start fresh session
- Telegram message → inject via stdin (stopping timer if Idle) → Working
- Alert from Swiftward → same as Telegram
- `/clear` → kill, discard history, next session starts without `--continue`
- Process crash → Killed, timer restarts, can resume via `--continue`

**Concurrency**: 4 goroutines - main loop, background session I/O, Telegram drain, Telegram poller. Shared state via atomics (`sessionActive`, `clearRequested`), mutexes (`tgMu`, `sessionCancelMu`), and channels (`sessionDoneCh`, `tgWake`, `TurnDoneCh`).

**Error handling**: `/clear` kills do not increment the consecutive-error counter; real errors do, halting the agent after `maxConsecutiveErrors`. Crashes retry with exponential backoff.

## Alpha and Gamma agents

### agent-alpha-claude

- Profile: `alpha` (`make up PROFILES=alpha`)
- Model: `claude-sonnet-4-6`
- Interval: 15 minutes (fast cycles)
- Prompt: `prompts/agent-alpha-claude/CLAUDE.md`
- Agent identity: `Swiftward Alpha`, agentId=32
- Philosophy: momentum trader, relative strength vs BTC, regime-based deployment, asymmetric R:R, cash is a position, process over outcome

### agent-gamma-claude

- Profile: `gamma` (`make up PROFILES=gamma`)
- Model: `claude-sonnet-4-6`
- Interval: 30 minutes (slower, more deliberate)
- Prompt: `prompts/agent-gamma-claude/CLAUDE.md`
- Agent identity: `Swiftward Gamma`, agentId=43
- Architecture: spawns subagents via Claude's native `Agent` tool - Technical / Sentiment / Market Structure analysts plus Bull / Bear researchers. Multi-perspective debate before a trading decision.

## Key files

**Go harness**:
- `golang/internal/agents/claude_runtime/executor.go` - Executor struct, Run(), stream-json protocol
- `golang/internal/agents/claude_runtime/loop.go` - event loop, state machine, timer, Telegram injection
- `golang/internal/agents/claude_runtime/lifecycle.go` - session startup/cleanup, signal handling
- `golang/cmd/claude-agent/main.go` - binary entry point

**Container**:
- `docker/claude-agent/Dockerfile` - two-stage build, Claude Code v2.1.87 pinned
- `docker/claude-agent/entrypoint.sh` - credential bootstrapping, settings init, `.mcp.json` generation

**Policy**:
- `swiftward/policies/rulesets/inet/v1/rules.yaml` - domain allow / block
- `swiftward/policies/rulesets/llm/v1/` - prompt injection, attention recovery
- `swiftward/policies/rulesets/mcp/v1/` - per-agent tool permissions, eval fixtures

**Architecture reference**:
- `docs/architecture/network-topology.md` - network isolation and port map
- `docs/architecture/claude-runtime-state-machine.md` - state transitions and concurrency invariants

## Notes

- **"Agents don't know they're controlled"** - Claude runs unmodified. Proxy interception, MCP routing, and policy enforcement are transparent. Policy verdicts are final; no LLM re-negotiates them.
- **Why Claude's native `Agent` tool for Gamma**: no framework, zero dependencies, well-tested, natural fit for multi-perspective debate.
- **Why stdin never closes**: this is the only way to inject Telegram and alert messages into an Idle session without cold-starting a new one.
- **Credential isolation**: per-agent `.claude` directories prevent cross-agent settings or auto-memory leakage.
- **Known limits**: gateway scanning latency is a few hundred ms per LLM call, which matters for very tight cycle times but is negligible at 15- and 30-minute intervals. Classifier models run on CPU; GPU acceleration is future work.
