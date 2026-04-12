# typescript

TypeScript subsystem: the **Trading Dashboard** — a React 19 + Vite + Tailwind 4 single-page app that talks to the Go trading server over its MCP HTTP endpoints via TanStack Query. Runs at http://localhost:8091 as the operator UI.

## Key files — pages

One screen per page, routed in [`src/App.tsx`](src/App.tsx).

- [`src/pages/Overview.tsx`](src/pages/Overview.tsx) (`/`) — home screen: agent roster, portfolio summary, recent trades, P&L at a glance.
- [`src/pages/AgentDetail.tsx`](src/pages/AgentDetail.tsx) (`/agents/:id`) — per-agent drill-down: session log, open positions, trade history, alerts, equity curve.
- [`src/pages/Market.tsx`](src/pages/Market.tsx) (`/market`) — market data: prices, candles, order books, funding rates across tracked pairs.
- [`src/pages/Evidence.tsx`](src/pages/Evidence.tsx) (`/trust`) — ERC-8004 evidence and agent trust view: decision hash chain, on-chain attestation status, per-agent trust rows.
- [`src/pages/Policy.tsx`](src/pages/Policy.tsx) (`/policy`) — Swiftward policy screen: active rules, shadow rules, rejection log with rule-trigger inference.
- [`src/pages/Demo.tsx`](src/pages/Demo.tsx) (`/demo`) — demo mode: side-by-side comparison with guardrails toggled on and off.
- [`src/pages/ClaudeAgent.tsx`](src/pages/ClaudeAgent.tsx) (`/claude-agent`) — Claude Code session history and live alerts feed per agent.
