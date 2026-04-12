# golang

Go subsystem: the platform's spine. A single multi-role binary (`cmd/server`) hosts the trading server, the MCP tool surfaces, the dashboard, and the Go-native trading agents — plus a separate binary for the Claude Code trading agent (`cmd/claude-agent`) and a set of on-chain operations CLIs. Roles are selected at runtime via the `TRADING__ROLE` env var (comma-separated).

## What's inside

- **Trading server** (`cmd/server` role `trading_mcp`) — order submission, policy-gated execution, fill tracking, portfolio, ERC-8004 attestation pipeline.
- **MCP servers** (`cmd/server` roles `risk_mcp`, `market_data_mcp`, `news_mcp`, `files_mcp`, `code_mcp`) — Go-hosted tool namespaces: `risk/*`, `market/*`, `news/*`, `files/*`, `code/*`.
- **Dashboard** (`cmd/server` role `dashboard`) — HTTP API and embedded SPA.
- **Claude Code trading agent** ([`cmd/claude-agent/`](cmd/claude-agent/)) — the primary trading agent, backing `agent-alpha-claude` and `agent-gamma-claude`. Runs Claude Code itself as the brain: spawns `claude --print --output-format stream-json` as a child process, parses the NDJSON event stream, manages session lifecycle, and injects alerts as inline messages between turns.
- **Go LLM agent** (`cmd/server` role `llm_agent`) — OpenAI-compatible chat loop against the MCP tool surface. Backs `agent-delta-go`.
- **Random agent** (`cmd/server` role `random_agent`) — dummy autonomous trade generator used as a baseline. Backs `agent-random`.
- **Agent intel CLI** ([`cmd/agent-intel/`](cmd/agent-intel/)) — on-chain hackathon intelligence pipeline: sync events and state, compute PnL, generate the audit HTML report.
- **On-chain ops CLIs** — one-shot tools: [`cmd/chain-check/`](cmd/chain-check/), [`cmd/gen-wallets/`](cmd/gen-wallets/), [`cmd/leaderboard-spy/`](cmd/leaderboard-spy/), [`cmd/erc8004-setup/`](cmd/erc8004-setup/).

## Key files

### Trading server & trade engine

- [`internal/mcps/trading/service.go`](internal/mcps/trading/service.go) — order submission, fill tracking, policy + RiskRouter gates, portfolio state, ERC-8004 attestation lifecycle.
- [`internal/swiftward/evaluator.go`](internal/swiftward/evaluator.go) — Swiftward policy client over gRPC: synchronous per-trade `EvaluateSync` plus async logging.
- [`internal/exchange/kraken_client.go`](internal/exchange/kraken_client.go) — Kraken execution client in live and paper modes, wrapping the Kraken CLI binary.

### MCP servers

- [`internal/mcps/risk/service.go`](internal/mcps/risk/service.go) — `risk/*` tools: halt, resume, status.
- [`internal/mcps/marketdata/service.go`](internal/mcps/marketdata/service.go) — `market/*` tools: quotes, candles, indicators, price alerts.
- [`internal/mcps/news/service.go`](internal/mcps/news/service.go) — `news/*` tools: search, sentiment, keyword alerts.
- [`internal/mcps/files/service.go`](internal/mcps/files/service.go) — `files/*` tools: workspace-isolated read / write / list / delete (10 MB per-write cap).
- [`internal/mcps/codesandbox/service.go`](internal/mcps/codesandbox/service.go) — `code/*` tools: Python execution inside per-agent persistent Docker sandboxes.

### ERC-8004 & chain

- [`internal/chain/client.go`](internal/chain/client.go) — Ethereum client: EIP-712 signing, nonce management, gas bumping, rate-limit retry.
- [`internal/evidence/decision_hash.go`](internal/evidence/decision_hash.go) — keccak256 decision hash chain over canonical decision JSON plus the previous hash.

### Claude Code trading agent

- [`internal/agents/claude_runtime/executor.go`](internal/agents/claude_runtime/executor.go) — child-process management: stream-json NDJSON parsing, idle timeout, output fan-out.
- [`internal/agents/claude_runtime/loop.go`](internal/agents/claude_runtime/loop.go) — session state machine: event dispatch, alert injection, triage pre-filter, wake-up cooldown.
- [`internal/agents/claude_runtime/lifecycle.go`](internal/agents/claude_runtime/lifecycle.go) — session tracking: start / result capture, error classification (auth, rate-limit).
- [`internal/agents/claude_runtime/telegram.go`](internal/agents/claude_runtime/telegram.go) — Telegram operator bridge: wake-message injection and `/clear` kill.

### Go LLM agent

- [`internal/agents/simple_llm/service.go`](internal/agents/simple_llm/service.go) — OpenAI-compatible chat loop with MCP tool dispatch (Qwen, GLM, DeepSeek).
- [`internal/agents/simple_llm/tools.go`](internal/agents/simple_llm/tools.go) — MCP toolset adapter: maps tool-name prefixes to MCP clients, converts to OpenAI function-calling format.

### Dashboard

- [`internal/dashboard/dashboard.go`](internal/dashboard/dashboard.go) — HTTP router, embedded SPA, static serving with SPA fallback.
