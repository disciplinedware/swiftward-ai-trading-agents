## Context

`trading_mcp.domain.entity` (Task 7) provides the ORM models. `trading_mcp.infra.db` provides the async engine and session factory. This task adds the domain service that reads and writes those models.

The service is called by the FastAPI server (Task 11) and the paper engine (Task 9). It owns all portfolio state logic — callers pass a session, the service does the work.

## Goals / Non-Goals

**Goals:**
- `PortfolioService` class injected with `async_sessionmaker`
- All five query methods from plan.md Task 8
- Unrealized and realized PnL calculation
- Drawdown: (peak_total_usd - current_total_usd) / peak_total_usd, from latest portfolio_snapshot
- Daily PnL: sum of realized_pnl_usd for positions closed today (UTC)
- asyncio.Lock protecting all write operations

**Non-Goals:**
- No MCP server wiring (Task 11)
- No price fetching (service accepts current_prices as a parameter — caller provides them)
- No paper engine execution (Task 9)

## Decisions

### Service receives current_prices as a parameter
`get_positions()` and `get_portfolio()` need current prices to compute unrealized PnL. The service does not call price_feed-mcp — that would create a circular dependency and make the service non-testable without a running server. Callers pass `current_prices: dict[str, Decimal]`. For tools that don't need prices (get_position by asset, get_daily_pnl), no price parameter.

### asyncio.Lock per service instance
A single `asyncio.Lock` on the `PortfolioService` instance guards all write paths (open_position, close_position, save_snapshot). Reads are lock-free — reads are always within a transaction and SQLAlchemy async sessions are not thread-safe but are coroutine-safe when used sequentially within one session scope.

### Drawdown from latest portfolio_snapshot
Rolling peak is stored in `portfolio_snapshots.peak_total_usd`. The service reads the most recent row. If no snapshots exist yet, drawdown = 0.0.

### Daily PnL resets at UTC midnight
Filter: `Position.closed_at >= today_utc_start AND Position.status == "closed"`. The service computes `today_utc_start` using `datetime.now(timezone.utc).replace(hour=0, minute=0, second=0, microsecond=0)`.

### Return types: dataclass DTOs, not ORM rows
The service returns simple dataclass instances (not ORM entities) so callers don't need SQLAlchemy session context. DTOs: `PortfolioSummary`, `PositionView`. These are defined in `trading_mcp/domain/dto.py`.

## Risks / Trade-offs

[No live current prices in tests] → Tests inject a fixed `current_prices` dict. Unrealized PnL tested with synthetic data. This is correct — the service should not fetch prices.

[asyncio.Lock is instance-level] → If multiple `PortfolioService` instances exist (shouldn't happen), the lock doesn't protect across instances. Mitigation: single instance created at server startup (Task 11).

## Open Questions

None.
