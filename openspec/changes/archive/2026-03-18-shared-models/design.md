## Context

Task 1 (scaffolding) is complete: `src/common/config.py` uses Pydantic `BaseModel` throughout, and `Decimal` is established as the convention for financial values (per CLAUDE.md). Task 2 extends this foundation with the shared data models. These models are the single source of truth for the shapes that cross component boundaries via JSON-RPC.

The project is Python 3.13 + Pydantic v2 + pytest with `asyncio_mode = auto`. No additional dependencies needed.

## Goals / Non-Goals

**Goals:**
- Define `TradeIntent`, `Position`, `SignalBundle` as Pydantic `BaseModel` subclasses
- Support lossless JSON-RPC round-trip (`model_dump()` / `model_validate()`) for all three
- Enforce `Decimal` for all financial values, serialized as `str` at the JSON boundary
- Provide typed `Literal` constraints for enumerated string fields
- Keep `SignalBundle` sub-classes as stubs — structure finalized during MCP tasks (3–6)
- Re-export everything from `src/common/models/__init__.py`

**Non-Goals:**
- ORM mapping (Postgres models live in `trading-mcp`, not here)
- Full `SignalBundle` field definitions (deferred to MCP implementation)
- Any business logic inside models

## Decisions

### D1: Pydantic BaseModel over `@dataclass`

Plan.md says "dataclasses" but Task 1 already uses Pydantic for config. Using `BaseModel` gives `.model_dump()` / `.model_validate()` for the JSON-RPC round-trip requirement without any manual serialization code. All downstream tasks (brain, MCP servers, backtesting) benefit from built-in validation.

**Alternative considered**: `@dataclass` + manual `to_dict()` / `from_dict()` — rejected because it adds code with no benefit given Pydantic is already a dependency.

### D2: `Decimal` for financial fields, serialized as `str`

CLAUDE.md requires `Decimal` for all prices, sizes, PnL, balances. JSON has no `Decimal` type, so the boundary encoding must be chosen. `str` is lossless (unlike `float`) and unambiguous on the receiving end.

Implementation: custom Pydantic field serializer using `field_serializer` or a model-level `model_serializer` that converts `Decimal` → `str` on `model_dump()` and accepts `str | Decimal | int | float` on `model_validate()`.

**Alternative considered**: serialize as `float` — rejected, lossy and violates CLAUDE.md rule.

### D3: Single `Position` class with Optional closed-state fields

A closed position is just an open position with additional fields populated (`closed_at`, `exit_price`, `realized_pnl_usd`, `realized_pnl_pct`, `exit_reason`, `tx_hash_close`). A `status: Literal["open", "closed"]` field distinguishes them. This avoids parallel class hierarchies and simplifies the stop-loss loop which only cares about `entry_price`, `stop_loss`, `take_profit`.

**Alternative considered**: `OpenPosition` / `ClosedPosition` subclasses — rejected, adds inheritance complexity for a simple distinction.

### D4: `SignalBundle` sub-classes as stubs

The exact fields for `PriceFeedData`, `OnchainData`, `NewsData`, `FearGreedData` depend on what each MCP server returns, which is defined in Tasks 3–6. Stubbing them now with `pass` bodies lets the brain and tests compile without committing to a field shape that may change.

The dict-of-dicts structure (`prices: dict[str, PriceFeedData]`) is decided now because it's an architectural choice, not a field-level detail.

## Risks / Trade-offs

- **Decimal serialization friction**: Every component reading a `model_dump()` payload gets `str` for financial fields and must parse back to `Decimal`. This is the correct tradeoff (lossless), but callers must be aware. Mitigated by `model_validate()` accepting `str` transparently.
- **SignalBundle stubs create temporary incomplete types**: mypy/pyright will flag `PriceFeedData` as having no fields until Tasks 3–6 fill them in. Acceptable — stubs signal "come back here."
- **Interface stability pressure**: CLAUDE.md notes these must not change lightly. Any field rename after Tasks 3–6 start is a breaking change across agent, MCP servers, and backtesting. Mitigated by careful spec review before implementing MCP tasks.

## Open Questions

- None blocking implementation. `SignalBundle` field structure is deliberately deferred.
