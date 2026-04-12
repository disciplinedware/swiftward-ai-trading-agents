## Context

`PortfolioService` (Task 8) handles all DB writes. `TradeIntent` (Task 2) is the input model. The paper engine sits between them: it takes a TradeIntent, prices the trade, and calls `PortfolioService.open_position()` or `close_position()`.

The engine does NOT call price_feed-mcp directly in this task — it receives a `current_price` parameter. The MCP server (Task 11) will fetch the price and pass it in. This makes the engine fully testable without HTTP.

## Goals / Non-Goals

**Goals:**
- `PaperEngine` class with `execute_swap(intent, current_price)` method
- Fill price = current_price × (1 + 0.001) for LONG opens, current_price for FLAT closes
- LONG path: validate max_concurrent_positions; open new Position + Trade + Snapshot
- FLAT path: close existing open position + Trade + Snapshot (no-op if no open position)
- Returns `ExecutionResult(status, tx_hash, executed_price, slippage_pct, reason)`
- `tx_hash` always `paper_<uuid4>`
- `status`: `"executed"` on success, `"rejected"` if limit hit or no position to close

**Non-Goals:**
- Live engine (Task 11)
- Price fetching (caller provides current_price)
- ERC-8004 hooks (Task 10)

## Decisions

### Engine receives current_price, not fetches it
Separation of concerns: the engine is pure trading logic, not HTTP. Task 11's server lifespan calls price_feed-mcp and passes current_price. This makes the engine 100% testable without network.

### Snapshot calculation
After a LONG open: `total_usd = stablecoin_balance - size_usd`, `stablecoin_balance -= size_usd`, `open_position_count += 1`, peak = max(peak, total), drawdown = (peak - total)/peak.
After a FLAT close: `total_usd = stablecoin_balance + size_usd + realized_pnl_usd`, `stablecoin_balance += size_usd + realized_pnl_usd`, `open_position_count -= 1`.
Realized PnL: `(exit_price - entry_price) / entry_price × size_usd`.
PaperEngine takes `starting_balance_usdc` and reads the latest snapshot to get current state.

### max_concurrent_positions check
PaperEngine takes `max_concurrent_positions: int`. Before opening: counts open positions. If at limit → return `ExecutionResult(status="rejected", reason="max_concurrent_positions reached")`.

### Trade row always written
Every `execute_swap` writes one `Trade` row. LONG open → direction="open". FLAT close → direction="close". If FLAT with no open position → return status="rejected", no DB writes.

### ExecutionResult dataclass
```python
@dataclass
class ExecutionResult:
    status: str          # "executed" | "rejected"
    tx_hash: str         # "paper_<uuid>" or ""
    executed_price: Decimal
    slippage_pct: Decimal
    reason: str          # "" on success, rejection reason on reject
```

## Risks / Trade-offs

[No atomicity between price fetch and fill] → In paper mode, this is acceptable. Price is sampled once by caller, passed to engine. Real-world slippage is approximated by the fixed 0.1% factor.

[Portfolio snapshot balance math] → Balance drift is possible if the latest snapshot's stablecoin_balance is stale. Mitigation: always read latest snapshot before computing new one.

## Open Questions

None.
