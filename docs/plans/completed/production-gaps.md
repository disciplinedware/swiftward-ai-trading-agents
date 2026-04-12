# Production Readiness - Gaps Closed

> **Status**: ✅ All hackathon-scope gaps closed

## Summary

Every production-readiness gap identified in the original plan is now closed. The trading system is multi-instance safe, forensically auditable, and uses strict decimal math throughout. No hackathon-scope item is deferred - the list below describes what was delivered and where to find the code.

## Closed

- **Decimal precision for monetary values** - all monetary fields use `shopspring/decimal.Decimal`, never `float64`. DB columns are `NUMERIC(20,8)` with a pgx codec for proper serialization. Config-level float64 values (read from YAML / env) are converted at the service boundary. Files: `golang/internal/config/config.go`, `golang/internal/db/pgx_repository.go`.

- **Advisory lock unlock on context cancel** - DB advisory-lock release uses `context.Background()` rather than the caller context, so a cancelled HTTP request cannot leak a lock. File: `golang/internal/db/pgx_repository.go`.

- **Deterministic trade history ordering** - history queries use `ORDER BY timestamp DESC, id DESC` so same-timestamp rows have a stable order. File: `golang/internal/db/pgx_repository.go`.

- **Halt flag is DB-backed, not per-process** - the `halted` column on the `agents` table is the single source of truth. All instances see the same state. File: `golang/internal/db/repository.go`.

- **Halt serialization** - `SetHalted` acquires the per-agent advisory lock before writing, so concurrent `submit_order` and `SetHalted` calls are serialized. No race window where a trade slips through between the check and the set. File: `golang/internal/mcps/trading/service.go:236-244`.

- **Decimal-safe Risk MCP serialization** - risk MCP response encoders call `.String()` on every decimal value before handing it to `json.Marshal`, so no precision is lost on the wire. File: `golang/internal/mcps/risk/service.go:170-172`.

- **Evidence chain is DB-backed, not in-process** - `traceStore` and `hashChain` were moved out of in-memory maps into the `decision_traces` Postgres table. Multi-instance safe; parity is asserted by `TestInsertTraceLatestByInsertionOrder`. Files: `golang/internal/db/repository.go`, `golang/internal/db/pgx_repository.go`, `golang/internal/db/mem_repository.go`, `golang/internal/db/repository_test.go`.

- **Nonce fallback removed** - the in-memory nonce fallback map (which could generate duplicates across instances or after restarts) is gone. On nonce errors the on-chain submission is skipped while the trade still fills on the exchange. File: `golang/internal/mcps/trading/service.go`.

- **Input validation on submit / estimate** - `submit_order` and `estimate_order` reject any `side` other than `"buy"` or `"sell"` with an explicit error. Test: `TestEstimateInvalidSide` in `golang/internal/mcps/trading/service_test.go`.

- **Cash overdraw guard** - `MemRepository.RecordTrade` rejects trades that would drive cash negative, matching the Postgres `CHECK (cash >= 0)` constraint. Test: `TestRecordTradeCashOverdraw`. File: `golang/internal/db/mem_repository.go`.

- **Trace ordering by insertion sequence** - `GetLatestTraceHash` orders by `seq DESC` on a `BIGSERIAL` column, eliminating clock-skew and same-timestamp ambiguity when looking up the previous hash for chain continuation. File: `golang/internal/db/pgx_repository.go:738-745`.

- **Evidence endpoint error differentiation** - `GET /v1/evidence/{hash}` returns 404 for `ErrTraceNotFound` and 500 for any other error (with zap logging). The sentinel error is defined on the repository interface so the MCP handler can cleanly differentiate. Files: `golang/internal/mcps/trading/service.go:serveEvidenceHash`, `golang/internal/db/repository.go` (`var ErrTraceNotFound`).

## Notes

- Decimal precision is enforced at three layers: the DB schema (NUMERIC), the Go type system (`decimal.Decimal`), and the JSON serializer (`.String()`).
- Concurrency safety is enforced by a combination of advisory locks (halt races), DB constraints (cash overdraw), and monotonic sequences (evidence chain forks).
- The evidence chain is now forensically complete - every decision has a predecessor, and 404 vs 500 on the evidence endpoint is unambiguous for external auditors.
