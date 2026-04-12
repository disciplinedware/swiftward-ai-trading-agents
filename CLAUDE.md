# CLAUDE.md - AI Trading Agents Platform

Polyglot platform for AI trading agents with policy-enforced risk management and on-chain evidence via ERC-8004. Hackathon project (March 30 - April 12, 2026).

## Quick Start

```bash
cp .env.example .env
make up PROFILES=random        # core stack + random trader (no agents without PROFILES)
make up PROFILES=random,alpha  # random + Claude alpha (needs Claude Code on host)
make up PROFILES=ruby          # + Ruby agent (Ruslan) on :7175
make up PROFILES=java          # + Java agent (Ivan)
make local-up PROFILES=random  # also builds swiftward from source (Kostya dev)
make down                      # stop full stack
```

**URLs**: Dashboard http://localhost:8091 | Control UI http://localhost:5174 | Ruby Agent http://localhost:7175 | SigNoz http://localhost:3301 | MCP Gateway http://localhost:8095/mcp/* | LLM Gateway http://localhost:8093/v1

## Key Commands

```bash
# Build
make swiftward-build-local     # build swiftward-server:local image
make swiftward-publish         # build multi-platform + push to GHCR
make trading-server-build      # build trading-server:local
make sandbox-build             # build sandbox-python:local (for Code MCP)

# Test & Lint
make golang-test               # unit tests (no running stack needed)
make golang-test-integration   # integration tests (needs Docker + sandbox image)
make golang-lint               # golangci-lint
make test                      # all languages
make lint                      # all languages

# Agents
make demo-ruby                 # start Agents Arena (Ruby)
make demo-java                 # start Java Gamma agent

# Logs
make logs                      # tail all service logs

# Ruby / Python / TypeScript
make ruby-test                 # Run Ruby tests
make python-test               # pytest -v
make typescript-dev            # Vite dev server
```

Run `make help` for the full list.

## Coding Conventions

### HARD RULE: No Backward Compatibility
No legacy code, no shims, no deprecated APIs, no fallback paths for old callers. Remove the old path completely.

### General
- No over-engineering. Hackathon project - minimum complexity for the current task.
- Tests: table-driven style. Single test function with array of cases. No test-per-case explosion.
- No `time.Now()` in rules or policy evaluation - use event timestamps for determinism.
- Never commit `.env`, API keys, private keys, or credentials.

### Go
- Logger: **zap only** (no fmt.Printf, no log.Println, no slog)
- Logging style: useful info in message string, fields for standard/repeating data. Message must be informative on its own.
- Config: **koanf only** (no Viper)
- Router: **chi** (stdlib-compatible)
- Errors: wrap with context (`fmt.Errorf("submit trade: %w", err)`), don't swallow
- Financial math: **decimal.Decimal** (shopspring) for trading values, `*big.Int` in wei for on-chain. Never `float64` for money. Log monetary values as strings.
- Single binary in `cmd/server/`, all packages in `internal/`. Role selected by `TRADING__ROLE` env var.
- MCP protocol: JSON-RPC 2.0 over HTTP. Tools namespaced: `trade/submit_order`, `market/get_prices`, etc.

### Ruby Agent (Agents Arena)
When working with Ruby/Rails, backtesting, swarm agents, or http://localhost:7175 — see `ruby/agents/solid_loop_trading/CLAUDE.md` for code layout, key models, and docker debugging.

### Ruby / Python / TypeScript
- Ruby: rubocop, rake test, agents in `ruby/agents/{name}/`
- Python: ruff (not flake8/pylint), pytest, agents in `python/agents/{name}/`
- TypeScript: npm, React + Vite, `npm run lint`

## Doc Maintenance Rules

Keep docs current - they are our specs and source of truth.

- **[docs/plans/completed/](docs/plans/completed/)**: Delivered features — each file describes what shipped, with file:line references.
- **[docs/mcps/*.md](docs/mcps/)**: MCP tool specs — update when implementation diverges.
- **[docs/architecture/overview.md](docs/architecture/overview.md)**: System overview and diagrams.
- **[docs/architecture/services.md](docs/architecture/services.md)**: Docker services, MCP endpoints, env vars, tech stack.
- **[docs/official-links.md](docs/official-links.md)**: External links and references.

## Team

| Person | Focus |
|--------|-------|
| Kostya | Claude Code agent, Go MCP servers, Trading Dashboard UI, Swiftward policies, ERC-8004 |
| Ruslan | Swarm agents with backtesting, agent selection, agent observability |
| Tikhon | Python orchestrated agent (fixed-flow + AI steps), Python MCP servers |
| Ivan | Website, slides, demo video, presentation & design |
