# AI Trading Agents Platform

> Polyglot multi-agent trading platform with policy-enforced risk management and on-chain evidence via ERC-8004.
> Submission for the Surge Hackathon (March 30 - April 12, 2026).

**Landing page**: [ai-trading.swiftward.dev](https://ai-trading.swiftward.dev/) - features, demo video, screenshots, team.

**Hackathon**: [AI Trading Agents on lablab.ai](https://lablab.ai/ai-hackathons/ai-trading-agents)

---

## Prerequisites

- **Docker** and **Docker Compose** v2+ (tested on Docker Desktop 4.x and Docker Engine 27.x)
- **8 GB RAM minimum** (SigNoz observability stack + trading services + agents)
- **make** (GNU Make)
- macOS or Linux (tested on both; Windows via WSL2 should work but untested)

For specific agents:
- **Alpha / Gamma / Midas**: [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated on the host (`claude login`)
- **Delta / Epsilon**: `OPENAI_API_KEY` in `.env` (or compatible endpoint)

No API keys needed for the simplest demo - the `random` agent works with zero external dependencies.

---

## Quick Start

```bash
git clone https://github.com/disciplinedware/swiftward-ai-trading-agents.git
cd swiftward-ai-trading-agents
cp .env.example .env
make up PROFILES=random        # core stack + random trader agent
```

The random agent starts trading immediately with simulated Kraken paper prices. Open the **Dashboard** at http://localhost:8091 to see live trades, positions, and P&L.

`make up` without PROFILES starts only infrastructure (no agents). Add agents via comma-separated profiles:

```bash
make up PROFILES=random,alpha  # random + Claude Alpha (needs Claude Code on host)
make up PROFILES=random,delta  # random + Delta Go LLM (needs OPENAI_API_KEY)
make down                      # stop stack
```

**URLs** (after `make up`):

| Service | URL |
|---------|-----|
| Dashboard (trades, positions, P&L) | http://localhost:8091 |
| Control UI (Swiftward policy management) | http://localhost:5174 |
| Ruby Agent UI (when `ruby` profile active) | http://localhost:7175 |
| SigNoz (logs, traces, metrics) | http://localhost:3301 |

Run `make help` for all available targets.

---

## Agents

Every agent is profile-gated - no agents start without `PROFILES`. Combine with commas: `make up PROFILES=random,alpha`. All env vars have working defaults in `.env.example` unless marked **required** below.

### Random Trader (Go) - `random`

Baseline deterministic agent. Trades random orders on a 60s interval across ETH, BTC, SOL, LINK, SUI. **No API keys needed** - works immediately after `cp .env.example .env`.

```bash
make up PROFILES=random
```

### Claude Alpha - `alpha`

Autonomous Claude Code agent with Swiftward policy enforcement. Runs on 15-min intervals, up to 96 sessions/day.

**Required:**
- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated on the host (`claude login`)

The Makefile auto-extracts Claude credentials from the host (macOS Keychain or `~/.claude/.credentials.json` on Linux). No API key in `.env` needed - credentials are mounted into the container. Optional: set `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_ALPHA_CLAUDE_TOPIC_ID` in `.env` for Telegram alerts.

```bash
make up PROFILES=alpha
```

### Claude Gamma - `gamma`

Multi-subagent Claude trader. Same prerequisites as Alpha (Claude Code CLI + credentials). Runs on 30-min intervals, up to 60 sessions/day.

```bash
make up PROFILES=gamma
```

### Agents Arena (Ruby/Rails) - `ruby`

Backtesting arena where multiple trading strategies compete on historical data, and the best-performing agent is selected for live trading. Web UI at http://localhost:7175.

```bash
make up PROFILES=ruby
```

Rails keys (`RAILS_MASTER_KEY`, `RAILS_SECRET_KEY_BASE`) are pre-filled in `.env.example`. No external API keys needed. For code execution in sandboxes, also run `make sandbox-build` once to build the sandbox image.

### Midas - `midas` (LIVE TRADING - real money)

Claude agent trading real funds on Kraken. **Cannot be started with `make up`** - requires dedicated command:

```bash
# Required in .env:
#   KRAKEN_API_KEY=...
#   KRAKEN_API_SECRET=...
make live-up   # sets TRADING__EXCHANGE__MODE=kraken_real automatically
```

Same Claude Code prerequisites as Alpha. Initial balance intentionally capped at $100 for safety.

### Delta (Go LLM) - `delta`

Go-based LLM agent using OpenAI-compatible API. Runs on 5-min intervals, max 10 iterations per session.

**Required in `.env`:** `OPENAI_API_KEY`. Model configured via `AGENT_DELTA_GO_MODEL` (default: `gpt-5.4-mini`).

```bash
make up PROFILES=delta
```

### Epsilon (Rust LLM) - `epsilon`

Rust-based LLM agent using OpenAI-compatible API. Runs on 5-min intervals, max 50 iterations per session.

**Required in `.env`:** `OPENAI_API_KEY`. Model configured via `AGENT_EPSILON_RUST_MODEL` (default: `gpt-5.4-mini`).

```bash
make up PROFILES=epsilon
```

### Gamma Java (Fear & Greed Contrarian) - `java`

Spring Boot agent using Fear & Greed index for contrarian trades. 15-min tick interval. No API keys needed. Override tick interval with `AGENT_GAMMA_JAVA_TICK_INTERVAL_MS` in `.env`.

```bash
make up PROFILES=java
```

### Python Deterministic LLM - `python`

Orchestrated Python agent with dedicated MCP servers (price feed, news, on-chain data, fear & greed).

```bash
make up PROFILES=python
```

Agent env vars (`AGENT_PYTHON_DETERMINISTIC_LLM_*`) are pre-filled in `.env.example`.

### Shared optional config

These apply across all agents (none are required for the basic demo):

| Variable | Purpose |
|----------|---------|
| `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID` | Telegram alerts (shared bot) |
| `OPENAI_API_KEY` | LLM Gateway + moderation + Delta/Epsilon agents |
| `CLASSIFIER_HF_TOKEN` | Prompt injection detection ([HuggingFace gated model](https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M)) |
| `PRISM_API_KEY` | Market intelligence enrichment (sign up at [prismapi.ai](https://prismapi.ai), code `LABLAB`) |
| `CRYPTOPANIC_TOKEN` | News feed via CryptoPanic API |
| Blockchain vars (`CHAIN_*`) | ERC-8004 on-chain attestations (see `.env.example`) |

---

## On-Chain Agents

Every agent has its own identity on the ERC-8004 AgentRegistry (Base Sepolia). Each decision is EIP-712 signed by the agent's operator wallet and posted to the ValidationRegistry before execution.

| Agent ID | Agent Name | Operator Wallet |
|----------|------------|-----------------|
| 32 | Swiftward Alpha | [`0x6Cd7DdABD496b545bAE05a04044F2828C1395d13`](https://sepolia.etherscan.io/address/0x6Cd7DdABD496b545bAE05a04044F2828C1395d13) |
| 37 | Random Trader | [`0x7a2F2E58B93Ac448fF7D0e81C2756A3EfC7a15e0`](https://sepolia.etherscan.io/address/0x7a2F2E58B93Ac448fF7D0e81C2756A3EfC7a15e0) |
| 43 | Swiftward Gamma | [`0xC5e0362badA7D1968325e134783dA2B7c48FbF62`](https://sepolia.etherscan.io/address/0xC5e0362badA7D1968325e134783dA2B7c48FbF62) |
| 49 | Haia Trading Agent | [`0xFa7b27f6D316CC96d93F6b126D1acd8066E340B7`](https://sepolia.etherscan.io/address/0xFa7b27f6D316CC96d93F6b126D1acd8066E340B7) |

---

## Subsystems

Polyglot monorepo. Each subsystem has its own README with key files and implementation notes.

- [`golang/`](golang/README.md) - trading server, MCP servers, Go LLM agent, shared Go libraries
- [`python/`](python/README.md) - orchestrated Python agent and Python MCP servers (price feed, news, on-chain data, fear & greed)
- [`ruby/`](ruby/README.md) - Agents Arena: backtesting framework with strategy selection for live trading
- [`rust/`](rust/README.md) - Rust Epsilon LLM agent
- [`java/`](java/README.md) - Java Gamma agent
- [`typescript/`](typescript/README.md) - Trading Dashboard (React 19 + Vite + Tailwind)
- `swiftward/` - [Policy engine](https://swiftward.dev/)

---

## License

See [LICENSE](LICENSE).
