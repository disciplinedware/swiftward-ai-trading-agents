## 1. Engine module

- [x] 1.1 Create `src/trading_mcp/engine/__init__.py`
- [x] 1.2 Create `src/trading_mcp/engine/result.py` — `ExecutionResult` dataclass (status, tx_hash, executed_price, slippage_pct, reason)
- [x] 1.3 Create `src/trading_mcp/engine/paper.py` — `PaperEngine` class with constructor `(portfolio_service, max_concurrent_positions, starting_balance_usdc)`

## 2. execute_swap implementation

- [x] 2.1 Implement LONG path: check concurrent positions limit; compute fill_price = current_price × 1.001; build Position, Trade, PortfolioSnapshot; call portfolio_service.open_position(); return ExecutionResult(status="executed")
- [x] 2.2 Implement FLAT path: fetch open position for asset; if none → return ExecutionResult(status="rejected"); compute realized_pnl; build Trade, PortfolioSnapshot; call portfolio_service.close_position(); return ExecutionResult(status="executed")
- [x] 2.3 Implement portfolio snapshot calculation helper: reads latest snapshot state, computes new total_usd, stablecoin_balance, peak, drawdown

## 3. Tests

- [x] 3.1 Create `tests/trading_mcp/test_paper_engine.py` with SQLite in-memory fixture (engine + PortfolioService + PaperEngine)
- [x] 3.2 Test LONG fill price: current=2000 → executed_price=2002.00
- [x] 3.3 Test LONG writes Position (status=open) and Trade (direction=open)
- [x] 3.4 Test FLAT PnL: entry=2000, size=1000, exit=2200 → realized_pnl_usd=100, pct=0.10
- [x] 3.5 Test FLAT with no open position → status=rejected, no DB writes
- [x] 3.6 Test max_concurrent_positions: 2 open → 3rd LONG rejected
- [x] 3.7 Test ExecutionResult tx_hash format: starts with "paper_"

## 4. Verify

- [x] 4.1 Run `make lint` — no ruff errors
- [x] 4.2 Run `make test` — all tests pass
