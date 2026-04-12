# Evidence Chain

> **Status**: ✅ Shipped
> **Packages**: `golang/internal/mcps/trading/`, `golang/internal/chain/`, `golang/internal/evidence/`
> **UI**: `typescript/src/pages/Evidence.tsx`, `typescript/src/components/evidence/`
> **Storage**: PostgreSQL `decision_traces` table

## What was built

A tamper-evident, keccak256 hash-chained audit trail for every agent decision - both fills and rejects. Each decision is recorded in the `decision_traces` table with its hash linked to the previous one, creating an immutable chain per agent. Fills are additionally anchored on-chain via EIP-712-signed attestations posted to the ERC-8004 ValidationRegistry. The chain is publicly queryable via `GET /v1/evidence/{hash}` and visualized in the dashboard Trust / Evidence screen, so any observer can verify an agent's full decision history without trusting the backend.

## Data model

`decision_traces` table (`postgres/migrations/001_init.up.sql:34-43`):

| Column | Type | Purpose |
|--------|------|---------|
| `decision_hash` | `TEXT` (PK) | keccak256(canonical_json(trace) + previous_hash_bytes) |
| `agent_id` | `TEXT` | Which agent made the decision |
| `prev_hash` | `TEXT` | Hash of the previous decision in the chain (genesis = `0x000...000`) |
| `trace_json` | `JSONB` | Full decision payload: intent, policy verdict, fill/reject details |
| `created_at` | `TIMESTAMPTZ` | When the decision was recorded |
| `seq` | `BIGSERIAL` | Deterministic ordering; index on `(agent_id, seq DESC)` |

The hash is computed in `golang/internal/evidence/decision_hash.go:16-47`. Canonical JSON uses RFC 8785 (keys sorted lexicographically), so the same trace always produces the same hash on replay.

**Shape of `trace_json`**:

- Fills: `{intent, swiftward, risk_router, fill}` - full pipeline with policy verdict and fill details
- Rejects: `{intent, reject: {source, reason}, swiftward}` - why the trade was blocked

Attestation data (on-chain tx hash, status) is stored inline on the `trades.evidence.attestation` JSONB field until a dedicated `attestations` table lands.

## End-to-end flow

1. Agent calls `trade/submit_order(pair, side, value, params...)` on the Trading MCP
2. Service builds the intent, enriches with portfolio context, runs Swiftward policy eval
3. **Decision hash computed** via `evidence.ComputeDecisionHash(trace, previous_hash)`
4. **Decision trace inserted** into `decision_traces` in the same DB transaction as the `trades` row update (`golang/internal/db/pgx_repository.go`)
5. **For fills: EIP-712 attestation**:
   - Build `TradeCheckpointData` and compute `checkpointHash` (`golang/internal/chain/signer.go:111-184`)
   - Sign with agent wallet key
   - Call `validationReg.PostEIP712Attestation(ctx, agentKey, agentTokenID, checkpointHash, score, notes)` (`golang/internal/chain/identity.go:452-492`)
   - Store on-chain tx hash and status (`pending → waiting_for_gas → pending_confirm → success`) in `trades.evidence.attestation`
6. **For rejects**: local chain only (on-chain reject attestation is future work)
7. **Hash chain advances**: next trade's `prev_hash` points to this decision's hash automatically
8. **Public query**: `GET /v1/evidence/{hash}` returns hash, prev_hash, agent_id, created_at, and the full `trace_json`

## Key files

- `golang/internal/evidence/decision_hash.go:16-47` - `ComputeDecisionHash()`: keccak256 over canonical JSON + previous-hash bytes
- `golang/internal/mcps/trading/service.go:578` - attestation call site in the fill path
- `golang/internal/chain/identity.go:452-492` - `PostEIP712Attestation()` submitting to the ValidationRegistry
- `golang/internal/chain/signer.go:111-184` - `TradeCheckpointData` and the EIP-712 signing logic
- `golang/internal/db/pgx_repository.go` - `InsertTrace()` and the decision_traces transaction handling
- `golang/internal/mcps/trading/service.go:handleEvidenceRequest` - `GET /v1/evidence/{hash}` handler
- `postgres/migrations/001_init.up.sql:34-43` - decision_traces schema
- `postgres/migrations/008_evidence.up.sql:4` - evidence JSONB column on trades
- `postgres/migrations/009_attestation_state.up.sql` - migration marking legacy fills as attested/disabled

## UI

`typescript/src/pages/Evidence.tsx` renders the Trust screen. Components:

- `typescript/src/components/evidence/HashChain.tsx` - vertical hash chain visualization with a `CompactTimeline` variant. Each node is a trade (fills green, rejects red) with its truncated decision hash. Click to expand and fetch the full trace via `getEvidence(hash)` (`typescript/src/api/evidence.ts`).
- `typescript/src/components/evidence/ValidationLog.tsx` - aggregated table of all trades with validation scores and proof status.
- `typescript/src/components/evidence/ReputationScores.tsx` - win rate, total return, compliance, max drawdown metrics.
- `typescript/src/types/api.ts` - `EvidenceTrace` type mirrors the Go `DecisionTrace` struct.

## Public evidence endpoint

`GET /v1/evidence/{hash}` returns:

```json
{
  "hash": "0xabc...",
  "prev_hash": "0xdef...",
  "agent_id": "agent-alpha",
  "created_at": "2026-04-08T03:28:33Z",
  "data": { /* full trace_json */ }
}
```

No auth - any observer can audit any agent's chain. Served on a configurable port separate from the main MCP server for isolation.

## Notes

**Shipped**:
- Local hash chain for all fills and rejects
- Canonical JSON (RFC 8785) for reproducible hashes across replay
- EIP-712 attestation to ERC-8004 ValidationRegistry for fills
- Public `GET /v1/evidence/{hash}` endpoint
- Dashboard hash chain visualization
- Attestation recovery loop on startup for crash resilience
- Per-agent attestation circuit breaker to prevent chain hammering on transient errors

**Known limits / future work**:
- Rejects not yet attested on-chain (policy-block attestations are planned)
- Attestation strategy is hardcoded to `on_fill`; interval batching and Merkle-tree batch attestation are deferred
- Dedicated `attestations` table (separate from inline JSONB) is planned
- No subgraph / indexer yet for filtering on-chain attestations
