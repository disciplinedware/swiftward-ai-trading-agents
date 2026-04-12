# Trade DB - Design Decisions (Mar 2026)

Decisions made during the PostgreSQL persistence layer implementation for Trading MCP.

## Trade ID: BIGSERIAL (not UUID)

Trades are append-only, single Postgres instance, IDs are internal (never exposed to external systems). BIGSERIAL gives optimal B-tree locality for `ORDER BY timestamp DESC` queries, 8 bytes vs 16, and easier debugging ("trade 4217" vs a UUID). UUID would be justified for distributed writes or externally-visible IDs - neither applies here.

## No `positions` table - compute from trades

Open positions are derived from the `trades` table on every query (sum buys - sells per market). With hackathon volumes (<1000 trades), this is instant. Avoids dual-write complexity and the inevitable consistency bugs between a positions table and the trade log.

## No `portfolio_snapshots` table - use value_after

Equity curve is stored as `value_after` on each trade record. For `get_portfolio_history`, query trades ordered by timestamp. Time-bucketed snapshots can be added later via a cron job if needed.

## Cost basis: running position tracking with reset on flat

Computing cost basis went through 4 iterations:

1. Net cost (buys - sells) - wrong, doesn't track proportional reduction
2. Sum of buys only - wrong after close-and-reopen (old buy costs contaminate new position)
3. **Running position with proportional reduction and flat reset** (final, correct):
   - Buy: `costBasis += value`, `qty += buyQty`
   - Sell: `fraction = sellQty / currentQty`, `costBasis -= costBasis * fraction`, `qty -= sellQty`
   - Flat (qty <= 0.0001): reset both to 0
   - This correctly handles partial sells AND close-and-reopen sequences

Both `PgxRepository` and `MemRepository` use identical logic. The pgx version fetches trades chronologically (`ORDER BY timestamp ASC`) and computes the running position.

## Distributed per-agent lock (pg_advisory_lock)

`submit_order` acquires a Postgres advisory lock per agent before reading state, evaluating policy, executing on exchange, and persisting. This serializes the full trade flow across all trading-server instances - no instance can pass policy checks on stale state while another is mid-trade.

Implementation: `LockAgent` acquires a dedicated pgxpool connection, calls `pg_advisory_lock(fnv64(agent_id))`, and returns an `unlock` function that calls `pg_advisory_unlock` + releases the connection. Different agents trade in parallel (different lock IDs).

The MemRepository implementation is a no-op (tests don't need distributed locking).

## RecordTrade: delta-based updates (not absolute writes)

`RecordTrade` accepts a `StateUpdate` with deltas, not absolute values:

```go
type StateUpdate struct {
    AgentID       string
    CashDelta     decimal.Decimal // added to cash (negative for buys)
    PeakValue     decimal.Decimal // GREATEST(current, this) applied
    FillCountIncr int             // added to fill_count (0 or 1)
    RejectIncr    int             // added to reject_count (0 or 1)
}
```

SQL uses relative updates: `cash = cash + $2, fill_count = fill_count + $4`. This is safe across multiple trading-server instances - no stale-read overwrites. Two instances reading the same state and writing concurrently both apply their deltas correctly.

Previous approach (absolute writes with `SELECT ... FOR UPDATE` row lock) was still vulnerable: the state was read before the transaction, so the locked update still wrote stale absolute values.

The transaction still wraps state update + trade insert atomically to prevent orphaned states.

## peak_value: never rolls back

Both implementations preserve peak consistently:

1. **pgx**: `GREATEST(peak_value, $3)` in `RecordTrade`, `UpdateAgentState`, and conditional WHERE in `UpdatePeakValue`
2. **MemRepository**: `if existing.PeakValue > updated.PeakValue { updated.PeakValue = existing.PeakValue }` in both `UpdateAgentState` and `RecordTrade`

`UpdatePeakValue` is a targeted conditional update: `UPDATE ... SET peak_value = $2 WHERE agent_id = $1 AND peak_value < $2`. Used by heartbeat to avoid full state overwrites.

## Heartbeat: targeted update, not full state overwrite

Heartbeat recomputes equity from current prices and updates peak_value if higher. It does NOT overwrite the full agent state - uses `UpdatePeakValue` which only touches peak_value with a conditional WHERE clause. This is safe without the per-agent mutex.

After the conditional update, heartbeat re-reads the agent from DB to get the authoritative peak_value. This handles the case where another concurrent request raised peak above the current equity - without re-read, the response would report a stale/lower peak and incorrect drawdown.

## Irreversible side effects: return success with persist_error

When an exchange fill succeeds but the subsequent DB write fails, we can't undo the trade. The response returns success (the trade happened) with a `persist_error` field so the caller knows persistence failed:

```json
{"status": "fill", "fill": {"qty": "1.0"}, "persist_error": "insert trade: connection refused"}
```

No retry - retrying a trade insert could create duplicates if the first attempt actually committed.

## Equity adjustment for current fill

When computing `value_after` for a trade, the fill hasn't been inserted to DB yet, so `ComputeEquity` doesn't include it. We manually adjust:

- Buy: `equity = baseEquity + filledQty * currentPrice` (new asset not yet in DB)
- Sell: `equity = baseEquity` (sell proceeds already in cash via state update)

## Migrations: Docker compose service (not auto-migrate from Go)

Migrations use the `migrate/migrate` Docker image as a compose service, matching the swiftward-core pattern:

```yaml
trading-pg-migrations:
  image: migrate/migrate
  command: ["-path=/migrations", "-database", "postgres://...", "up"]
  volumes:
    - ./postgres/migrations:/migrations:ro
  depends_on:
    postgres:
      condition: service_healthy
```

The trading-server depends on `trading-pg-migrations: condition: service_completed_successfully`. Go code never touches schema.

## Rejected trades stored in DB

Rejected trades are inserted with `status=reject`, null price/equity. This feeds compliance metrics: `compliance_pct = fill_count / (fill_count + reject_count)` and `guardrail_saves = reject_count`. Both are posted to ERC-8004 Reputation Registry.

## Event enrichment for Swiftward

Trading MCP enriches trade events with portfolio context before sending to the policy engine:

```go
eventData := map[string]any{
    "order": map[string]any{
        "pair":  pair,
        "side":  side,
        "value": value,
    },
    "portfolio": map[string]any{
        "value": equity,
        "peak":  state.PeakValue,
        "cash":  state.Cash,
    },
    "fill_count": state.FillCount,
}
```

This lets Swiftward rules evaluate position limits and drawdown without querying external systems - the policy engine stays stateless with respect to portfolio data.

## Trade replay ordering: timestamp + id tiebreaker

`GetOpenPositions` computes running cost basis by replaying trades chronologically. To ensure deterministic results when multiple trades share the same timestamp, we order by `timestamp ASC, id ASC`. `GetTradeHistory` uses `timestamp DESC, id DESC`. Without the `id` tiebreaker, Postgres can return same-timestamp trades in any order, producing unstable cost basis results.

## Credential redaction in logs

Connection strings are redacted using `url.Parse` + strip `User` before logging:

```go
func redactConnString(s string) string {
    u, err := url.Parse(s)
    if err != nil { return "***" }
    u.User = nil
    return u.String()
}
```

Simple regex-based approaches miss edge cases (passwords with `@`, special chars).

## Multi-instance safety

The design targets horizontal scaling - multiple trading-server instances sharing the same Postgres.

**Cash overdraw prevention**: A `CHECK (cash >= 0)` constraint on `agent_state` prevents two instances from both passing policy checks on stale state and then both deducting cash. The second instance's `RecordTrade` will fail with a constraint violation, and the exchange fill is reported with `persist_error`. Combined with delta-based updates (`cash = cash + $delta`), this ensures the DB never enters an invalid state.

**DB-backed nonce**: `chain_nonce` column on `agent_state`, atomically incremented via `UPDATE ... SET chain_nonce = chain_nonce + 1 RETURNING chain_nonce - 1`. No process-local state - safe across instances. On DB error, on-chain submission is skipped entirely (no in-memory fallback - fallback would risk duplicate nonces across instances/restarts).

**Advisory lock**: `submit_order` holds a `pg_advisory_lock` for the full policy-check + exchange + persist path, preventing stale-state policy bypass across instances.

**Evidence chain**: `decision_traces` table stores hash-chained decision traces. `GetLatestTraceHash` uses `ORDER BY seq DESC` on a `BIGSERIAL` column for deterministic ordering (not timestamps, which can skew across instances). `InsertTrace` omits `created_at` (Postgres `DEFAULT NOW()` assigns it from the DB server clock) and uses `ON CONFLICT DO NOTHING` for idempotency. On transient DB errors, the evidence chain is skipped entirely rather than silently resetting `prev_hash` to genesis (which would fork the chain). The evidence HTTP endpoint queries DB directly.

**Agent halt flags**: Moved from process-local `atomic.Bool` to a DB `halted` column on `agent_state`. `SetHalted` acquires the per-agent advisory lock before writing, so it serializes with in-flight `submit_order` calls - no race window where halt returns success but a concurrent trade still passes. All instances see the flag through their next `GetAgent` read (inside the advisory lock).

## Evidence API: typed not-found error

The evidence endpoint (`GET /v1/evidence/{hash}`) must distinguish "hash doesn't exist" (404) from "DB is down" (500). A sentinel `ErrTraceNotFound` error is returned by both `PgxRepository` and `MemRepository` for genuine not-found cases. The handler checks `errors.Is(err, db.ErrTraceNotFound)` to choose the status code.

Without this, a transient DB outage would return 404 for every evidence lookup, making real outages look like missing evidence to auditors.

## Advisory lock: context-safe unlock

The advisory lock unlock closure uses `context.Background()` instead of the caller's context. If the caller's HTTP request context is cancelled before `defer unlock()` runs, using the original context would silently fail the unlock - leaking the lock until the pooled connection is recycled. Using Background ensures the unlock always executes.
