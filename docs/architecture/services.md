# Services & Infrastructure Reference

## Docker Services

| Service | Port | Role | Network |
|---------|------|------|---------|
| `postgres` | 5432 (host: 5435) | PostgreSQL 18 - shared DB (trading, swiftward, ruby agent) | default + agent-isolated |
| `redis` | 6379 | Cache (news, prices, agent state) | default |
| `classifiers` | (internal) | ML classifiers for prompt injection detection (PG2 + BERT) | default |
| `swiftward-server` | **5174** (Control UI), **8093** (LLM GW), **8095** (MCP GW), **8097** (Inet GW) | Policy engine + Control UI + LLM/MCP/Inet Gateways | default + agent-isolated |
| `trading-server` | **8091, 8092** | Trading/Risk/Files/Code/Market/News MCPs + Evidence API + Dashboard UI | default + agent-isolated |
| `signoz-otel-collector` | 4317 (gRPC), 4318 (HTTP) | OTEL collector - receives traces/metrics/logs from all services | default + agent-isolated |
| `signoz` | **3301** (UI) | Observability UI (traces, metrics, logs) | default |
| `agent-random` | - | Random trader demo agent (profile: `random`) | default |
| `agent-alpha-claude` | - | Claude Code agent - autonomous trader + Swiftward (profile: `alpha`) | agent-isolated |
| `agent-gamma-claude` | - | Claude Code agent - multi-subagent trader (profile: `gamma`) | agent-isolated |
| `agent-gamma-java` | - | Java Fear & Greed agent (profile: `java`) | default (TODO: isolate) |
| `agent-ruby-solid` | **7175** | Agents Arena - backtesting + strategy selection (profile: `ruby`) | default + agent-isolated |
| `agent-ruby-solid-worker` | - | Ruby GoodJob worker - executes trading jobs (profile: `ruby`) | agent-isolated |

**Every agent is profile-gated** — none is always-on. To start agents, pass a comma-separated `PROFILES` list to `make up`:

```bash
make up PROFILES=random                       # random agent only
make up PROFILES=random,alpha                 # random + Claude alpha
make up PROFILES=alpha                        # alpha only (no random)
make up PROFILES=random,ruby                  # random + Agents Arena
make up PROFILES=random,java                  # random + Java
```

Use `make local-up` instead of `make up` when making changes to `swiftward-core` — it rebuilds the swiftward image from source. Everything else is identical.

Stop everything: `make down`.

The Go binary (`golang/cmd/server`) runs as ALL backend services - role selected by `TRADING__ROLE` env var: `trading_mcp,risk_mcp,market_data_mcp,files_mcp,code_mcp,news_mcp`, `random_agent`, or `llm_agent`. The Claude agent is a separate binary (`golang/cmd/claude-agent`) that spawns Claude Code CLI as a subprocess. Swiftward server is a separate binary with its own roles: `ingestion,worker,control_api,control_ui,mcp_gateway,llm_gateway,inet_gateway` + subcommands `migrate` and `load-policies`.

## MCP Endpoints

**Backend** (trading-server:8091):
- `POST /mcp/trading` - Trading MCP
- `POST /mcp/risk` - Risk MCP
- `POST /mcp/files` - Files MCP
- `POST /mcp/code` - Code Sandbox MCP
- `POST /mcp/market` - Market Data MCP
- `POST /mcp/news` - News MCP

**MCP Gateway** (swiftward-server:8095) - proxies to backend with optional policy evaluation:
- `POST /mcp/trading` - Trading MCP (guardrails: enabled, stream: trading)
- `POST /mcp/market` - Market Data MCP (guardrails: enabled, stream: mcp)
- `POST /mcp/news` - News MCP (guardrails: enabled, stream: mcp)
- `POST /mcp/polymarket` - Polymarket MCP (guardrails: disabled)
- `POST /mcp/files` - Files MCP (guardrails: disabled, timeout: 10s)
- `POST /mcp/code` - Code Sandbox MCP (guardrails: disabled, timeout: 0s - unlimited for sandbox execution)

**LLM Gateway** (swiftward-server:8093) - dual-upstream proxy (OpenAI + Anthropic):
- `POST /v1/chat/completions` - routes to OpenAI upstream
- `POST /v1/responses` - routes to OpenAI upstream
- `POST /v1/messages` - routes to Anthropic upstream
- `POST /{entity_id}/v1/*` - same as above, extracts entity_id from path (for clients that can't send X-Agent-ID header)
- `key_source=client` - agents send their own API keys, gateway forwards as-is
- Claude agents set `ANTHROPIC_BASE_URL=http://swiftward-server:8093/{agent_id}`

## Architecture Details

- **LLM Gateway** (inside swiftward-server:8093): Dual-upstream HTTP proxy (OpenAI + Anthropic) with policy evaluation. Routes by endpoint: `/v1/messages` to Anthropic, `/v1/chat/completions` and `/v1/responses` to OpenAI. Optional `/{entity_id}/v1/*` path prefix for clients that can't send X-Agent-ID headers (e.g. Claude Code CLI). Two key_source modes: `client` (agent sends its own API key, gateway forwards it) and `managed` (gateway selects key from DB via rules). We use `client` mode - agents send real API keys, gateway runs injection detection via classifiers (Llama PG2 + BERT), logs to Investigation, forwards as-is.
- **MCP Gateway** (inside swiftward-server:8095): proxies agent tool calls to upstream Trading MCP with policy evaluation. `fail_open` mode - requests pass through even if rules don't match the event type (Phase 1). Agents connect here instead of directly to trading-server.
- **Observability**: SigNoz (ClickHouse-backed). OTEL Collector receives traces/metrics/logs from all services, exports to SigNoz ClickHouse. SigNoz UI at :3301.
- **Policy Engine**: YAML rulesets with v1 (active, enforced) + v2 (shadow, logged only). Decision hash chain (keccak256). Agent halt mechanism. Swiftward is a policy ENGINE, not a data collector - it evaluates events it receives, never fetches external data (determinism/replay).
- **Event enrichment**: Trading MCP enriches trade events with portfolio context (equity, position_pct, drawdown_pct) before sending to Swiftward. This lets rules evaluate position limits and drawdown without Swiftward needing to query external systems.
- **Trade persistence**: PostgreSQL is the single source of truth for trades and portfolio state. No in-memory cache. Enables horizontal scaling of Trading MCP instances.
- **Trading MCP**: agent-facing tools (submit_order, estimate_order, get_portfolio, get_history, get_limits, get_portfolio_history, heartbeat). Also posts reputation feedback on-chain using validator key.
- **Risk MCP**: operator-facing API for Dashboard UI (halt/resume agents, status). NOT for agents - agents learn constraints from Trading MCP responses.
- **LLM Agent**: Go agent using OpenAI tool-calling to autonomously trade. Loads a strategy prompt, discovers MCP tools, runs a tool-calling loop until the LLM decides to stop. Connects to LLM Gateway + MCP Gateway (trading/files/market/code). `{{memory}}` placeholder preloads `memory/MEMORY.md` and session logs via Files MCP before the session. Logger hierarchy: `llm_agent` (root) > `llm_agent.init` (pre-session prefetch) > `llm_agent.llm` (session loop).
- **Market Data MCP**: read-only agent-facing data (prices, candles, orderbook, funding rates, open interest, price alerts). Backed by a `DataSource` interface with four implementations: `simulated` (GBM-based), `binance` (real data, rate-limited), `bybit` (real data, alternative source), and `composite` (primary source with simulated fallback). Default sources: `kraken,bybit`. Server-side indicator engine (EMA, SMA, RSI, MACD, BBands, ATR, VWAP). In-memory alert poller checks prices every `TRADING__MARKET_DATA__ALERT_POLL_INTERVAL` (default 10s). Runs as role `market_data_mcp` inside trading-server, exposed at `POST /mcp/market`.
- **"With vs Without" demo**: change agent's `MCP_URL` from `swiftward-server:8095` (MCP Gateway, with guardrails) to `trading-server:8091` (direct, without guardrails).
- **Dashboard UI**: React SPA embedded in trading-server Go binary via `go:embed`. Served at `/` on port 8091 (same port as MCP endpoints). SPA routing via catch-all that serves `index.html` for non-static paths. Calls MCP endpoints directly via JSON-RPC (`/mcp/trading`, `/mcp/risk`, `/mcp/market`, `/mcp/news`) and Evidence API (`/v1/evidence/{hash}`). Six screens: Overview, Agent Detail (portfolio/trades/limits), Market, Evidence, Policy, With/Without Demo. Runs as role `dashboard` inside trading-server. For local dev with hot reload: `cd typescript && npm run dev` (Vite proxies to localhost:8091).

## MCP Tool Namespaces

5 agent-facing MCP servers + 1 operator API, 33 tools total. Full specs in [mcps/](mcps/).

| MCP | Prefix | Audience | Tools | Priority |
|-----|--------|----------|-------|----------|
| Trading | `trade/` | Agent | submit_order, estimate_order, get_portfolio, get_history, get_portfolio_history, get_limits, heartbeat | P0 |
| Market Data | `market/` | Agent | get_prices, get_candles, get_orderbook, list_markets, get_funding, get_open_interest, set_alert, list_alerts, cancel_alert | P0 |
| Code Sandbox | `code/` | Agent | execute (install via subprocess inside execute) | P0 |
| Files | `files/` | Agent | read, write, edit, append, delete, list, find, search | P0 |
| News | `news/` | Agent | search, get_latest, get_sentiment, get_events | P0 |
| Risk | `risk/` | Operator (Dashboard) | halt_agent, resume_agent, get_agent_status, list_agents | P0 |

## Tech Stack

| Component | Technology | Version |
|-----------|-----------|---------|
| Platform | Go | 1.25 |
| Router | chi | v5 |
| Config | koanf | v1 |
| Logger | zap | - |
| Agents | Go (LLM), Ruby, Python | latest |
| LLM Client | go-openai (sashabaranov) | latest |
| Dashboard | React 19 + Vite + TypeScript + TailwindCSS v4 + TanStack Query + Recharts + React Router v6 | latest |
| Database | PostgreSQL | 18 |
| Policy Engine | Swiftward (Docker image) | latest |
| On-chain | ERC-8004 on Sepolia | - |

## Environment Variables

Key vars from `.env.example`:

| Variable | Default | Purpose |
|----------|---------|---------|
| `OPENAI_API_KEY` | - | OpenAI key for LLM agent |
| `TRADING__EXCHANGE__MODE` | kraken_paper | Exchange mode: `kraken_paper` (Kraken prices, fake fills), `paper` (Binance prices, fake fills), `sim` (GBM random walk), `kraken_real` (live Kraken trading) |
| `CHAIN_RPC_URL` | - | Sepolia RPC for ERC-8004 |
| `CHAIN_VALIDATOR_PRIVATE_KEY` | - | Wallet key for on-chain signing |
| `TRADING__MARKET_DATA__SOURCES` | kraken,bybit | Ordered list of data sources (CSV): `kraken`, `binance`, `bybit`, `simulated`. First is primary, rest are fallbacks. |
| `TRADING__MARKET_DATA__MARKETS` | ETH-USD,BTC-USD,SOL-USD | Markets tracked by the trading server |
| `TRADING__MARKET_DATA__VOLATILITY` | 80 | Annualized volatility % for GBM simulation |
| `TRADING__MARKET_DATA__CANDLE_HISTORY` | 500 | Pre-generated historical candles per interval (simulated) |
| `TRADING__MARKET_DATA__ALERT_POLL_INTERVAL` | 10s | How often to check price alerts |
| `TRADING__NEWS__SOURCES` | cryptocompare | News data source |
| `TRADING__NEWS__CRYPTOPANIC_TOKEN` | - | CryptoPanic API auth token (free tier) |
| `TRADING__LLM_AGENT__MODEL` | configurable (e.g. `gpt-4o-mini`, `claude-sonnet-4-6`) | LLM model for the Go LLM agent |
| `TRADING__LLM_AGENT__LLM_URL` | http://swiftward-server:8093/v1 | LLM Gateway base URL. If set, routes LLM calls through the gateway. If empty, calls OpenAI directly. |
| `TRADING__LLM_AGENT__MODE` | once | LLM agent run mode: `once`, `cli`, `server` |
| `TRADING__LLM_AGENT__INTERVAL` | 5m | Tick interval for server mode |
| `TRADING__LLM_AGENT__PROMPT_FILE` | ./prompts/delta/prompt.md | Strategy prompt file path |
| `TRADING__LLM_AGENT__FILES_MCP_URL` | http://swiftward-server:8095/mcp/files | Files MCP URL for LLM agent (memory/MEMORY.md preload + agent workspace) |
| `TRADING__LLM_AGENT__MARKET_DATA_MCP_URL` | http://swiftward-server:8095/mcp/market | Market Data MCP URL for LLM agent |
| `TRADING__CODE_MCP__HOST_WORKSPACE_PATH` | (empty) | Absolute host path for workspace bind-mounts into sandbox containers (set in .env) |
| `ANTHROPIC_BASE_URL` | http://swiftward-server:8093/{agent_id} | Anthropic LLM Gateway URL (set in Claude agent containers, entity_id in path) |
| `CLAUDE_AGENT__AGENT_ID` | - | Claude agent identifier (e.g. "agent-claude-alpha") |
| `CLAUDE_AGENT__INTERVAL` | 30m | Session interval (15m for simple agent) |
| `CLAUDE_AGENT__MODEL` | (claude default) | Claude model ID (e.g. "claude-sonnet-4-6") |
| `CLAUDE_AGENT__TELEGRAM_BOT_TOKEN` | - | Telegram bot token (empty = disabled) |
| `CLAUDE_AGENT__TELEGRAM_CHAT_ID` | 0 | Target Telegram chat ID |
| `CLAUDE_AGENT__TELEGRAM_TOPIC_ID` | 0 | Forum topic ID within chat (0 = no topic) |
| `CLAUDE_AGENT__TRADING_MCP_URL` | http://trading-server:8091/mcp/trading | Alert polling endpoint |
| `CLAUDE_AGENT__MARKET_MCP_URL` | http://trading-server:8091/mcp/market | Alert polling endpoint |

Swiftward uses a single server image (GHCR `ghcr.io/disciplinedware/ai-trading-agents/swiftward-server:latest`). Migrations, load-policies, and Control UI are all embedded in this image. Override with `:local` tag if building from source.

**Swiftward seeding**: LLM provider config lives in Swiftward's DB (not env vars). `swiftward/seed.sql` upserts the OpenAI provider, key (PLACEHOLDER), and gpt-4o model. The `swiftward-seed-api-keys` compose service injects the real `OPENAI_API_KEY` from `.env`. Both run automatically on `make up`. Auth is disabled on both gateways (`REQUIRE_AUTH: false`) - no Declarion in this stack.
