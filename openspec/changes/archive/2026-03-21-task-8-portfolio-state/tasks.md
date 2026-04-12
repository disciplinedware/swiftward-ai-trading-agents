## 1. DTOs

- [x] 1.1 Create `src/trading_mcp/domain/dto.py` with `PositionView` and `PortfolioSummary` dataclasses (all fields Decimal for financial values, datetime for timestamps, Optional where nullable)

## 2. Portfolio Service

- [x] 2.1 Create `src/trading_mcp/domain/portfolio_service.py` — `PortfolioService` class with `async_sessionmaker` injected in constructor + `asyncio.Lock`
- [x] 2.2 Implement `get_portfolio(current_prices: dict[str, Decimal]) -> PortfolioSummary` — fetches open positions, latest snapshot for drawdown, daily PnL
- [x] 2.3 Implement `get_positions(current_prices: dict[str, Decimal]) -> list[PositionView]` — open positions with unrealized PnL
- [x] 2.4 Implement `get_position(asset: str, current_price: Decimal) -> PositionView | None` — single open position
- [x] 2.5 Implement `get_daily_pnl() -> Decimal` — sum of realized_pnl_usd for positions closed since UTC midnight today
- [x] 2.6 Implement `open_position(position: Position, snapshot: PortfolioSnapshot) -> None` — inserts position + snapshot atomically under Lock
- [x] 2.7 Implement `close_position(position_id: int, exit_price: Decimal, exit_reason: str, realized_pnl_usd: Decimal, realized_pnl_pct: Decimal, snapshot: PortfolioSnapshot) -> None` — updates position + inserts snapshot atomically under Lock

## 3. Tests

- [x] 3.1 Create `tests/trading_mcp/test_portfolio_service.py` with SQLite in-memory fixture (reuse session setup from test_schema.py pattern)
- [x] 3.2 Test `get_portfolio` — empty portfolio returns starting balance; portfolio with open position includes unrealized PnL
- [x] 3.3 Test `get_positions` — unrealized PnL arithmetic (entry 200, current 220, size 1000 → pnl_usd=100, pct=0.10)
- [x] 3.4 Test `get_position` — found and not-found cases
- [x] 3.5 Test `get_daily_pnl` — sums today's closed positions; excludes yesterday's
- [x] 3.6 Test concurrent `open_position` — two concurrent calls both persist without error

## 4. Verify

- [x] 4.1 Run `make lint` — no ruff errors
- [x] 4.2 Run `make test` — all tests pass
