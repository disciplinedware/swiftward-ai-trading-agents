# Dashboard Trust & Evidence UI

> **Status**: ✅ Shipped
> **Source**: `typescript/src/pages/Evidence.tsx`, `typescript/src/components/evidence/`, `typescript/src/components/agent/EvidenceTab.tsx`

## What was built

The Evidence screen was reorganized into a Trust-first architecture with two complementary views: a **global Trust Overview** that aggregates cross-agent decision metrics, and a **per-agent Evidence tab** embedded in each agent's detail panel. Each agent keeps its own immutable hash chain, linked to decision traces in `decision_traces` and on-chain attestations on the ERC-8004 ValidationRegistry. Auditors and judges can verify any trade decision end-to-end without leaving the dashboard.

## Screens

### Trust Overview (`/trust`)

Global view for auditors and jury to assess platform-wide agent behaviour.

- **Agent Trust Summary table**: every agent with decision counts (total, fills, rejects) and hash-proof coverage. Agent IDs link through to the per-agent Evidence tab.
- **Cross-agent decision timeline**: compact visual of decisions across agents, coloured per agent for pattern recognition.
- **Validation log**: aggregated table of fills and rejects with validation scores and evidence proof status.
- **Summary metrics**: total agents, total decisions, total trades with cryptographic hash proof.

### Per-agent Evidence tab (`/agents/:id`, Evidence tab)

Focused view for operators deep-diving on one agent.

- **Per-agent decision timeline**: compressed chronological view of this agent's trades only.
- **Hash chain viewer**: vertical chain visualization. Each node expandable to show the full decision trace (intent, Swiftward verdict, risk router result, fill details). Traces are fetched on-demand from `/v1/evidence/{hash}`.
- **Reputation scores**: win rate, total return, compliance rate (approvals vs total intents), max drawdown. Single-agent mode, no dropdown.
- **Hash proof summary**: counts of total decisions vs decisions with cryptographic hash proof, so operators can spot audit gaps.

### Hash chain node detail

When a hash chain node is expanded:

- **Decision trace**: hierarchical display of the full record - intent parameters (pair, side, value, params), Swiftward risk verdict, on-chain tx hash, fill details (price, qty, fee).
- **Chain links**: current hash and previous hash, each with copy-to-clipboard and a link to the evidence API endpoint.
- **Status icons**: checkmark for fills, X for rejects - quick scannability.
- **JSON inspector**: syntax-highlighted raw decision data for auditors who need granular inspection.

## Components and files

- `typescript/src/pages/Evidence.tsx` - Trust Overview page. Fetches all agents, loads their trade history, computes the cross-agent summary, and renders the timeline and validation log.
- `typescript/src/components/agent/EvidenceTab.tsx` - Per-agent Evidence tab. Uses `useTradeHistory` to fetch a single agent's trades, renders per-agent timeline, hash chain, and reputation scores.
- `typescript/src/components/evidence/HashChain.tsx` - Vertical chain visualization (`HashChain` export) and compact variant (`CompactTimeline` export). Handles node expansion, on-demand trace fetching via `getEvidence(hash)`, and syntax highlighting.
- `typescript/src/components/evidence/ReputationScores.tsx` - Metric cards (win rate, total return, compliance, max drawdown). Single-agent mode when called from the per-agent tab.
- `typescript/src/components/evidence/ValidationLog.tsx` - Cross-agent validation table, filters fills with hash proofs, scores by proof status.
- `typescript/src/api/evidence.ts` - API client with `getEvidence(hash)` for the public `/v1/evidence/{hash}` endpoint.
- `typescript/src/components/Navigation.tsx` - Top navigation. Route `/trust` labelled "Trust" with the Fingerprint icon.

## Data sources

- **Trade history**: `GET /v1/trades/{agentId}?limit=200` - list of agent decisions (intent, status, fill details, `decision_hash`)
- **Evidence trace**: `GET /v1/evidence/{hash}` - full decision trace including intent, Swiftward verdict, risk router refs, fill details
- **Agent list**: `useAgents()` hook - iterated for cross-agent aggregation
- **Caching**: React Query. Trades refetched every 10s for real-time feel; evidence traces are immutable, so stale time is infinite.

## Notes

- **Hash chain is per-agent**: each agent has its own genesis block (`prev_hash` empty / zero). No cross-agent chaining.
- **Decision hash is optional at the UI level**: not every trade has a hash yet (rejects are partial), so the UI gracefully counts hashed vs total and labels gaps instead of hiding them.
- **Trust Overview deliberately omits the per-agent HashChain** to avoid clutter. Deep-dive is the domain of the per-agent tab.
- **Responsive layout**: hash chain and reputation scores sit side-by-side on desktop (lg breakpoint) and stack on mobile.
- **Renaming from Evidence to Trust** was intentional - "Trust" reads more naturally for non-engineers (jury, investors) looking at the screen.
