## 1. ExecutionResult and Engine Interface

- [x] 1.1 Add optional `size_usd: Decimal | None = None` field to `ExecutionResult` in `engine/result.py`
- [x] 1.2 Confirm `Engine` protocol in `engine/interface.py` is already correct (no DB deps in signature)

## 2. Slim Down PaperEngine

- [x] 2.1 Remove `PortfolioService`, `starting_balance_usdc`, and `max_concurrent_positions` from `PaperEngine.__init__`
- [x] 2.2 Rewrite `_open_long`: compute fill_price + size_usd (read balance from a passed Decimal, not from service), set `result.size_usd`, return ExecutionResult — no DB writes
- [x] 2.3 Rewrite `_close_flat`: return ExecutionResult with executed_price — no DB reads/writes (position lookup moves to TradingService/PortfolioService)
- [x] 2.4 Update `server.py` wiring: `PaperEngine()` constructed with no service args; pass `max_concurrent_positions` to `TradingService` instead

## 3. PortfolioService: record_open and record_close

- [x] 3.1 Add `can_open_position(max_positions: int) -> bool` — non-locking count read
- [x] 3.2 Add `record_open(intent, result, max_positions) -> bool` — under write lock: atomic count check, derive size_usd if result.size_usd is None, build Position/Trade/Snapshot, insert atomically
- [x] 3.3 Add `record_close(asset, result)` — under write lock: look up open position, compute PnL, update Position to closed, insert Trade + Snapshot; no-op if no open position
- [x] 3.4 Remove (or keep internal-only) `try_open_position` and `open_position` — these are replaced by `record_open`

## 4. TradingService Orchestration

- [x] 4.1 Add `max_concurrent_positions: int` to `TradingService.__init__`
- [x] 4.2 In `execute_swap` for LONG: call `portfolio_service.can_open_position(max_positions)` → return rejected result immediately if False
- [x] 4.3 After successful engine execution for LONG: call `portfolio_service.record_open(intent, result, max_positions)` → if returns False, return rejected (race condition path)
- [x] 4.4 After successful engine execution for FLAT: call `portfolio_service.record_close(asset, result)`
- [x] 4.5 Update `execute_flat_all` to call `portfolio_service.record_close` for each closed position

## 5. Tests

- [x] 5.1 Update `PaperEngine` tests: remove PortfolioService mock, assert no DB interaction, check result.size_usd is populated
- [x] 5.2 Add `PortfolioService.record_open` tests: at-limit rejection, successful open, size_usd derived when None
- [x] 5.3 Add `PortfolioService.record_close` tests: PnL calculation, no-op when no open position
- [x] 5.4 Update `TradingService` tests: verify capacity pre-check short-circuits engine call, verify record_open/record_close called after execution
