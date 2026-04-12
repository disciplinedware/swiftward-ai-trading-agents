## Context

`PaperEngine` currently owns both execution logic (fill price + slippage) and portfolio accounting (reading balance state, building domain objects, writing Position/Trade/Snapshot to DB). `LiveEngine` has no portfolio accounting at all — it only submits to the blockchain. The result: in live mode, `get_portfolio`, `get_positions`, and `get_daily_pnl` all return empty/zero state because nothing is ever written to the DB.

`TradingService` already holds both `engine` and `portfolio_service` as dependencies, making it the natural home for the orchestration bridge.

## Goals / Non-Goals

**Goals:**
- Engine is a thin execution boundary: compute fill price, submit trade (paper/chain/CEX), return `ExecutionResult`
- DB writes happen in `PortfolioService` after every successful execution, regardless of engine
- Portfolio reads (`get_portfolio`, `get_positions`) return correct data in both paper and live mode
- No behavioral change to the agent's observable API (same MCP tool responses)

**Non-Goals:**
- Handling partially-filled live orders (out of scope — Risk Router ABI is a stub)
- Reading portfolio state from the chain instead of DB
- Concurrent multi-agent portfolio isolation

## Decisions

### 1. `TradingService` orchestrates the full open/close cycle

`TradingService.execute_swap()` becomes:
```
1. capacity pre-check (fast reject before touching chain)
2. price fetch
3. engine.execute_swap() → fill_price + tx_hash
4. portfolio_service.record_open() or record_close()
```

**Why not keep accounting in `PaperEngine`?** The engine should not know about DB persistence. Swapping to a CEX engine in future should require zero changes to accounting.

**Why not a new service layer?** `TradingService` is already the orchestrator. Adding another wrapper is premature complexity.

### 2. `PortfolioService.record_open` / `record_close` replace `try_open_position` / `close_position`

New methods accept `(intent, result, current_price)` and internally:
- build Position, Trade, PortfolioSnapshot objects
- do the atomic capacity check + write inside the lock

**Why keep the atomic check in `PortfolioService`?** The pre-check in `TradingService` is a fast-path reject (avoids hitting the chain). But two concurrent LONGs could still both pass the pre-check — the DB-level atomic check under the lock is the true safety guarantee.

### 3. `ExecutionResult` gains optional `size_usd: Decimal | None`

Paper engine knows the exact fill size (stablecoin × size_pct after reading balance). Live engine returns `None` — `record_open` derives size_usd from balance state via `get_balance_state()` using `size_pct` from the intent.

**Why not always derive in `record_open`?** Paper's `size_usd` depends on the balance at execution time. `record_open` runs after the engine, so the balance is already "spent". Deriving it there would use the post-execution balance and produce a wrong value. The engine (paper) reads the pre-execution balance and must communicate the filled size.

For live mode, `size_usd` comes from `intent.size_pct × stablecoin_balance` read inside `record_open` — this is acceptable because live execution is atomic on-chain and the in-process balance will be consistent (single agent).

### 4. `PaperEngine` constructor no longer takes `PortfolioService`

After the refactor, paper engine only needs `_SLIPPAGE` and a way to generate tx hashes. The `starting_balance_usdc` and `max_concurrent_positions` constructor args also move out — the service layer owns those concerns.

**Implication**: `server.py` wiring changes: `PaperEngine` constructed with no args (or just slippage constant). `PortfolioService` still constructed the same way.

## Risks / Trade-offs

- **Race condition (concurrent LONGs in live mode)**: Two concurrent `execute_swap` calls could both pass the pre-check, both submit to chain, and both reach `record_open`. The second would be rejected by the DB-level atomic check — but an on-chain trade would be orphaned (position exists on-chain, not in DB). Mitigation: the agent is single-threaded by design; true concurrency is not expected. Acceptable for hackathon scope.
- **Balance state race for live size_usd**: `record_open` reads balance state to compute `size_usd` for live mode. If called rapidly, it reads the same pre-update balance twice. Mitigation: same asyncio.Lock that guards the write also guards the read inside `record_open`, so the second call sees the updated balance.
- **Paper engine loses `PortfolioService` injection**: `PaperEngine` becomes stateless. The `max_concurrent_positions` check migrates to `PortfolioService.record_open`. Tests that mock `PortfolioService` interactions with `PaperEngine` need to be rewritten against `PortfolioService.record_open` directly.
