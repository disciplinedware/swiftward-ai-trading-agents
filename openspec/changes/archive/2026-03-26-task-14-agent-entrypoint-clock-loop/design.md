## Context

Tasks 1–13 produced: five MCP servers, Postgres-backed portfolio, paper engine, ERC-8004 hooks, brain protocol + StubBrain, and cooldown gate. A single `TradingMCPClient` exists at `src/agent/mcp_client.py` with only `get_portfolio()`. There is no agent entrypoint and no trigger loops — the agent cannot run.

## Goals / Non-Goals

**Goals:**
- Agent process starts, validates environment, and runs the 15-minute clock loop end-to-end
- Five typed MCP client classes, one per server, in `src/agent/infra/`
- Cooldown gate pre-filters candidate assets before brain runs (not after)
- Noop stubs for the three loops that arrive in Tasks 18–20 (so `main.py` needs no edits later)

**Non-Goals:**
- SwiftWard `approved/modified/rejected` response handling (Task 24)
- Stop-loss, price-spike, and tier-2 loops (Tasks 18–20)
- Real brain / LLM calls (Tasks 15–17)

## Decisions

### One client class per MCP server (not one fat class)

Each server lives at a different URL, has different tool names, and returns different models. Separate classes keep test mocks small, make dependency injection explicit, and mirror the one-file-per-external-system pattern already used in `src/mcp_servers/*/infra/`.

### Cooldown pre-filter before signal gather

`is_cooldown_open` is a pure sync dict lookup — zero cost. Computing `allowed_assets` first means prices/indicators are only fetched for assets that can actually trade, not all 10. Portfolio is still fetched unconditionally (needed for FLAT intents and position cap check). This eliminates a separate post-gather filter step: the bundle is already scoped to allowed assets when it reaches the brain.

```
clock tick
  → portfolio = get_portfolio()                              ← error: log + skip cycle
  → check portfolio.open_position_count < max               ← fail: skip cycle
  → allowed = [a for a in tracked if is_cooldown_open(a)]   ← sync, free
  → gather signals:
      portfolio (again — internal DB, cheap)
      prices / onchain / news for `allowed` assets only      ← error: log + skip cycle
  → brain.run(bundle)   ← bundle.prices already scoped to allowed
  → sort: FLAT first, LONG second
  → submit all intents → record_trade(asset)
```

FLAT intents are unaffected: `StubBrain._flat_intents()` reads from `bundle.portfolio.open_positions`, not `bundle.prices`, so scoping prices never suppresses a position close.

### Signal gather failure skips the cycle

Partial data (e.g., news MCP down) could produce a misleading `SignalBundle` — the brain would rank assets without sentiment data or with a stale fear-greed score. Skipping the full cycle is safer than trading on incomplete signals.

### Portfolio fetch is part of signal gather (no separate pre-check)

`global_positions_ok()` on `CooldownGate` makes a redundant `get_portfolio` call. Since signal gather already fetches portfolio, the position count check is done inline from the returned bundle.

### Noop stubs for missing loops

```python
async def _noop_loop() -> None:
    await asyncio.sleep(math.inf)
```

`asyncio.gather(clock_loop(), _noop_loop(), _noop_loop(), _noop_loop())` keeps `main.py` stable — Tasks 18–20 only swap the import, never touch the gather call.

## Risks / Trade-offs

- **All-or-nothing signal gather** → if one MCP server is flaky, the agent misses cycles. Mitigation: each client logs the failing server by name; operator can observe which server is causing skips.
- **CooldownGate.is_cooldown_open is sync** — called in a list comprehension inside the async clock loop. Safe: it's a pure dict lookup with no I/O.
- **`execute_swap` response shape** — trading-mcp returns `{status, tx_hash, executed_price, slippage_pct}`. Clock loop logs the result but takes no branching action on it. Full SwiftWard response handling (Task 24) adds branching without changing this interface.
