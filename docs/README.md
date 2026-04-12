# Documentation

For project setup, conventions, and commands, see [CLAUDE.md](../CLAUDE.md).

## Architecture

System design, components, and internal APIs.

- [architecture/overview.md](architecture/overview.md) - System diagram, components, trust tiers, data flows
- [architecture/services.md](architecture/services.md) - Docker services, MCP endpoints, env vars, tech stack
- [architecture/exchange-api.md](architecture/exchange-api.md) - Exchange client interface (pluggable backends: Kraken, Binance, Bybit, simulated)
- [architecture/claude-runtime-state-machine.md](architecture/claude-runtime-state-machine.md) - Claude Code agent state machine (Working/Idle/Killed), event loop, concurrency model

## MCP Specs

Agent-facing and operator-facing tool specifications. 7 servers.

- [mcps/README.md](mcps/README.md) - Overview + Files MCP (8 tools) + Risk MCP (operator-only)
- [mcps/trading.md](mcps/trading.md) - Trading MCP (13 tools): orders, portfolio, limits, history, alerts, conditional orders, heartbeat, end_cycle
- [mcps/market-data.md](mcps/market-data.md) - Market Data MCP (8 core + 3 PRISM): prices, candles, orderbook, funding, OI, alerts, optional fear/greed/technicals/signals
- [mcps/code-sandbox.md](mcps/code-sandbox.md) - Code Sandbox MCP (1 tool): execute Python in isolated Docker containers
- [mcps/news.md](mcps/news.md) - News MCP (6 tools): search, latest, sentiment, events, alerts

## Models & Prompts

Data structures and agent prompt system.

- [models/trade-model.md](models/trade-model.md) - Trade data model: three-currency trades, fees, portfolio, fill model
- [models/agent-prompt.md](models/agent-prompt.md) - Prompt construction: strategy + market context + memory injection

## Blockchain

ERC-8004 on-chain identity, wallet setup, evidence chain.

- [blockchain/setup.md](blockchain/setup.md) - Wallet setup, contract deployment, agent registration, IPFS metadata

## Decisions

Architectural decisions with rationale and trade-offs.

- [decisions/trade-db.md](decisions/trade-db.md) - Trade DB design: advisory locks, delta updates, cost basis, evidence chain
- [decisions/stop-loss-strategy.md](decisions/stop-loss-strategy.md) - Two-tier SL/TP: native exchange stops + software polling, OCO groups

## Hackathon

Strategy, rules, gaps, demo script for the AI Trading Agents hackathon (March 30 - April 12, 2026).

- [hackathon-discord-qa.md](hackathon-discord-qa.md) - Compiled Q&A from the lablab Discord, verified against organizer answers and contract code
- [official-links.md](official-links.md) - External links: hackathon pages, ERC-8004 specs, SDKs, tools
- [hackathon/demo-script.md](hackathon/demo-script.md) - Demo walkthrough (12-15 min)

## Plans (Feature Catalog)

Every file below describes what was actually built, with file:line references to the real code. The index doubles as a feature catalog.

**Platform**
- [plans/completed/hackathon-website.md](plans/completed/hackathon-website.md) - Landing page at ai-trading.swiftward.dev (vanilla HTML + 30+ real screenshots + AgentIntel audit subsite)
- [plans/completed/trading-dashboard-ui.md](plans/completed/trading-dashboard-ui.md) - React 19 dashboard: 6 pages, embedded in trading-server via go:embed
- [plans/completed/workspace-upgrade.md](plans/completed/workspace-upgrade.md) - Unified per-agent workspace shared by Files, Code Sandbox, and Claude agent

**Trading core**
- [plans/completed/universal-trading-mcp.md](plans/completed/universal-trading-mcp.md) - Universal Trading MCP (13 tools) with Kraken / paper / sim client abstraction
- [plans/completed/kraken-exchange-client.md](plans/completed/kraken-exchange-client.md) - Kraken CLI adapter: paper + live modes, native stop orders, per-agent isolation
- [plans/completed/market-data-mcp.md](plans/completed/market-data-mcp.md) - Multi-source market data with server-side indicators and save_to_file
- [plans/completed/candle-pagination.md](plans/completed/candle-pagination.md) - Candle fetch + save_to_file (Ruby Arena paginates Binance klines for the backtesting warehouse)
- [plans/completed/backtesting-simulator.md](plans/completed/backtesting-simulator.md) - Ruby Arena parallel strategy evaluation + Claude Code ad-hoc Python backtests

**Agents**
- [plans/completed/llm-trading-agent.md](plans/completed/llm-trading-agent.md) - Go LLM trading agent (Delta) with OpenAI tool calling
- [plans/completed/claude-agent-internet-gateway.md](plans/completed/claude-agent-internet-gateway.md) - Jailed Claude Code agent + three gateways (Internet / LLM / MCP)
- [plans/completed/python-agent-go-trading-mcp.md](plans/completed/python-agent-go-trading-mcp.md) - Python deterministic agent (3-stage brain, 4 trigger loops, 5 MCPs) + Go Trading MCP adapter
- [plans/completed/code-sandbox-mcp.md](plans/completed/code-sandbox-mcp.md) - Per-agent persistent Python 3.12 container with pickle state
- [plans/completed/files-and-code-mcp.md](plans/completed/files-and-code-mcp.md) - Files MCP: 8 tools with per-agent path sandboxing

**Risk + evidence + on-chain**
- [plans/completed/realistic-risk-rules.md](plans/completed/realistic-risk-rules.md) - Trading policy ruleset v1: graduated tiers, heartbeat kill switches, loss streak pause
- [plans/completed/evidence-chain-redesign.md](plans/completed/evidence-chain-redesign.md) - keccak256 hash-chained decision trace + EIP-712 attestation to ERC-8004 ValidationRegistry
- [plans/completed/evidence-ui-reorganize.md](plans/completed/evidence-ui-reorganize.md) - Trust-first dashboard: global Trust Overview + per-agent Evidence tab
- [plans/completed/onchain-trading.md](plans/completed/onchain-trading.md) - ERC-8004 Identity / Validation / Reputation on Sepolia, EIP-712 signing, ERC-1271 AgentWallet
- [plans/completed/polymarket-ai-agent.md](plans/completed/polymarket-ai-agent.md) - Polymarket prediction markets MCP (read-only v1)
- [plans/completed/production-gaps.md](plans/completed/production-gaps.md) - Production-readiness gap list: all closed (decimal math, advisory locks, DB-backed state, cash overdraw guard)

## Researches

Background research and investigations. Grouped by topic.

### ERC-8004 & On-Chain

- [researches/erc8004/deep-implementation.md](researches/erc8004/deep-implementation.md) - ERC-8004 registries deep dive
- [researches/erc8004/on-chain-identity-patterns.md](researches/erc8004/on-chain-identity-patterns.md) - On-chain identity patterns for trading agents
- [researches/erc8004/implementation-guide.md](researches/erc8004/implementation-guide.md) - Step-by-step ERC-8004 implementation

### Agents & Multi-Agent

- [researches/agents/sdk-research.md](researches/agents/sdk-research.md) - 11 frameworks evaluated; decision: build our own
- [researches/agents/multi-agent-best-practices.md](researches/agents/multi-agent-best-practices.md) - Multi-agent architecture patterns
- [researches/agents/swarm-intelligence.md](researches/agents/swarm-intelligence.md) - Swarm intelligence for trading

### Risk Management

- [researches/risk/industry-practices.md](researches/risk/industry-practices.md) - Hedge fund risk practices (Millennium, FTMO, Kelly Criterion)

### Infrastructure

- [researches/infrastructure/kraken-cli.md](researches/infrastructure/kraken-cli.md) - Kraken CLI: 134 commands, paper trading, MCP server
- [researches/infrastructure/mcp-vs-cli.md](researches/infrastructure/mcp-vs-cli.md) - MCP vs CLI architecture for quant analysis
- [researches/infrastructure/sandbox-custom-tools.md](researches/infrastructure/sandbox-custom-tools.md) - Custom CLI tools for in-sandbox analytics
