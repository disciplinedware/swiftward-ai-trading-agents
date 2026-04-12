# Trading Dashboard UI

> **Status**: ✅ Shipped
> **Source**: `typescript/`
> **Served by**: trading-server via `go:embed` at `http://localhost:8091`
> **Stack**: React 19 + Vite + TypeScript + TailwindCSS v4 + TanStack Query v5 + React Router v6 + Recharts

## What was built

A single-page operator dashboard embedded directly in the Go `trading-server` binary via `go:embed`. It talks to the trading server's MCP HTTP endpoints (`/mcp/trading`, `/mcp/risk`, `/mcp/market`, `/mcp/news`) and the public evidence API (`/v1/evidence/{hash}`) to show real-time agent activity, portfolio state, market data, policy enforcement, and cryptographic decision audit. Dark theme, tabbed navigation, responsive. No backend beyond the trading-server - React talks directly to the MCP JSON-RPC HTTP endpoints.

## Pages

### Overview (`/`)

- Metrics bar: total agents, halted count, portfolio value, daily P&L, peak equity, drawdown
- Agents table: full roster with portfolio values, trade / rejection counts, halt and resume actions
- Equity chart: multi-agent equity curves (Recharts multi-line)
- Activity feed: recent trades with timestamps, markets, side, outcome, P&L

### Agent Detail (`/agents/:id`)

Four tabs:

- **Portfolio** - cash, positions, trade counts, P&L, portfolio value
- **Trades** - history table with fill / reject / pending filters, per-trade decision hash, evaluation ID
- **Evidence** - per-agent hash chain (delegates to the Trust components, single-agent mode)
- **Limits** - current risk limits, circuit breaker status, breach history

### Market (`/market`)

- Prices table: bid / ask / last, 24h change, high / low, volume per pair
- Funding rates table: funding %, annualized, signal, open interest per market
- News feed: latest crypto news with source and timestamp

### Evidence / Trust (`/trust`)

- Trust overview: global hash chain timeline, per-agent summary table (total / fills / rejects / hashed), counts of trades with cryptographic proof
- Validation log: decision hash entries with JSON trace viewer, external links to on-chain evidence API

### Policy (`/policy`)

- Active rules: V1 ruleset with names, descriptions, priorities
- Rejection log: timestamped rejections with reasons, agent, market, inferred rule match
- Shadow mode section: rules that would have rejected but were run shadow-only, impact per agent
- Event type badges (`trade_order`, `heartbeat`, `execution_report`), relative time, rule-based filtering

### Claude Agent (`/claude-agent`)

- Session table: recent Claude Code runs per agent, status, elapsed time, action summary
- Live alerts feed and Telegram integration status
- Auto-refresh every 10 seconds, agent selector dropdown

## Key components

```
typescript/src/
├── App.tsx                        - routing and query client
├── pages/
│   ├── Overview.tsx
│   ├── AgentDetail.tsx            - tabs: Portfolio / Trades / Evidence / Limits
│   ├── Market.tsx
│   ├── Evidence.tsx               - Trust Overview
│   ├── Policy.tsx
│   └── ClaudeAgent.tsx
├── components/
│   ├── Layout.tsx
│   ├── Navigation.tsx
│   ├── overview/
│   │   ├── MetricsBar.tsx
│   │   ├── AgentsTable.tsx
│   │   ├── EquityChart.tsx
│   │   └── ActivityFeed.tsx
│   ├── agent/
│   │   ├── AgentHeader.tsx
│   │   ├── PortfolioTab.tsx
│   │   ├── TradesTab.tsx
│   │   ├── EvidenceTab.tsx
│   │   └── LimitsTab.tsx
│   ├── market/
│   │   ├── MarketPricesTable.tsx
│   │   ├── FundingTable.tsx
│   │   └── NewsFeed.tsx
│   └── evidence/
│       ├── HashChain.tsx          - HashChain + CompactTimeline exports
│       ├── ValidationLog.tsx
│       └── ReputationScores.tsx
├── api/
│   ├── trading.ts
│   ├── risk.ts
│   ├── market.ts
│   ├── news.ts
│   └── evidence.ts
├── hooks/                         - TanStack Query wrappers per domain
└── types/api.ts                   - TypeScript mirrors of Go API structs
```

## Data flow

- **TanStack Query v5** manages all server state. Stale times: 3-10 seconds for active data (trades, portfolio, prices), infinite for immutable decision traces. `keepPreviousData` prevents UI flicker during refetch.
- **Mutations** (halt / resume an agent) invalidate related queries automatically.
- **React Router v6** handles SPA routing. The trading-server's embedded handler serves `index.html` for all unmapped paths so client-side routing works.
- **React Hot Toast** for success / error notifications.
- **No WebSocket / SSE** - polling at 3-10 second intervals is sufficient for hackathon cycle times.

## Build and deployment

- `npm run dev` - Vite dev server with proxy to `:8091` for `/mcp/*` and `/v1/*`
- `npm run build` - Vite outputs to `typescript/dist/`
- `golang/internal/dashboard/dashboard.go` uses `//go:embed` on `dist/` and serves the static assets + SPA fallback at the root of the trading-server HTTP mux
- Single-binary deployment: dashboard ships inside `trading-server:local`

## Key files

- `typescript/package.json` - React 19, Vite, TailwindCSS v4, TanStack Query, React Router, Recharts, Lucide icons
- `typescript/vite.config.ts` - alias `@` → `src/`, dev proxy to `:8091`
- `typescript/src/App.tsx` - root routing
- `typescript/src/pages/` - 6 page components listed above
- `typescript/src/api/*.ts` - thin MCP JSON-RPC wrappers
- `typescript/src/hooks/*.ts` - TanStack Query hooks per API domain
- `typescript/src/types/api.ts` - shared TS types
- `golang/internal/dashboard/dashboard.go` - `go:embed` integration

## Notes

- **Operator-only**: no authentication for the hackathon. Auth layer is future work.
- **Dark theme**: surface tokens (`surface-base`, `surface-hover`, `surface-border`, `text-primary`, `accent`) via Tailwind v4 design tokens.
- **Responsive**: desktop-first, tablet-usable. Mobile works but is not the primary target.
- **Public evidence API is read-only**: the dashboard reads it; it is not a write path.
- **Build size**: ~250KB gzipped including Recharts and Lucide.
- **Testing**: Vitest is wired up (`src/**/*.test.ts`), with TypeScript strict mode across the app.
