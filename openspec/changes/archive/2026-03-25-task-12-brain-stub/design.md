## Context

Tasks 1–11 built the full data and execution pipeline: MCP servers, portfolio persistence, paper/live engine, ERC-8004 hooks. Nothing orchestrates them yet. The agent needs a `Brain` — the component that consumes signals and produces `TradeIntent` objects. Tasks 13–20 (cooldown gate, clock loop, stop-loss, tier2, real LLM brain) all depend on a concrete `Brain` being importable and callable.

The stub brain is deliberately LLM-free and deterministic so it can be tested in isolation and used in backtesting without any external services.

## Goals / Non-Goals

**Goals:**
- Define the `Brain` protocol that all implementations (stub, deterministic_llm, future) share
- Implement `make_brain(config)` factory with `stub` and a forward-declaration for `deterministic_llm`
- Implement `StubBrain` covering all 3 stages using only the signal data already in `SignalBundle`
- Fill in the stub sub-models in `SignalBundle` so signal fields have concrete types
- Introduce `PortfolioSnapshot` as a clean common model (no trading_mcp import in common/)

**Non-Goals:**
- Any LLM calls — that is Task 15–17
- MCP client wiring (that is Task 14)
- Clock or trigger loops (that is Task 13–14)
- Backtesting stubs (Task 22)

## Decisions

### 1. Brain as a `typing.Protocol`, not ABC

**Decision**: Use `typing.Protocol` with `runtime_checkable`.

**Rationale**: Protocol allows structural subtyping — any class with `async def run(self, signal_bundle) -> list[TradeIntent]` satisfies the contract without importing from `base.py`. This makes backtesting stubs trivial to write and keeps the interface non-intrusive. ABC would require explicit inheritance, creating a coupling that doesn't add value.

### 2. `PortfolioSnapshot` in `common/models/`, not imported from `trading_mcp`

**Decision**: Define a new `PortfolioSnapshot` Pydantic model in `src/common/models/portfolio_snapshot.py`.

**Rationale**: `trading_mcp.domain.dto.PortfolioSummary` is a dataclass internal to the trading MCP domain. Importing it into `common/` would create an upward dependency (`common` → `trading_mcp`) that violates the layering rule. `PortfolioSnapshot` carries only the fields the brain actually needs: open positions (asset + key levels), counts, and balances. It's a Pydantic model so it round-trips cleanly as JSON — matching how MCP responses are deserialized.

### 3. Filling in `PriceFeedData`, `FearGreedData`, `NewsData` as part of Task 12

**Decision**: Define concrete fields on all three stubs in this task, not later.

**Rationale**: The stub brain's health score and asset ranker formulas require specific fields from these models. Leaving them as `pass` means every test would have to mock empty objects, and any real computation would fail with `AttributeError`. The MCP servers already exist and return well-defined shapes — aligning the models now is the right time, before any brain logic is tested.

Fields:
- `PriceFeedData` per asset: `price`, `change_1m`, `change_5m`, `change_1h`, `change_4h`, `change_24h`, `rsi_14`, `ema_20`, `ema_50`, `ema_200`, `atr_14`, `bb_upper`, `bb_mid`, `bb_lower`, `volume_ratio` (all `str`, decoded to `Decimal` in the brain)
- `FearGreedData`: `value` (int), `classification` (str), `timestamp` (str)
- `NewsData` per asset: `sentiment` (float, −1.0–1.0), `macro_flag` (bool)

### 4. FLAT intent placeholder values

**Decision**: `stop_loss=Decimal("0")`, `take_profit=Decimal("0")`, `reasoning_uri="stub://"`.

**Rationale**: FLAT intents close positions — the stop/take fields are semantically unused. Trading-mcp treats FLAT as an immediate exit regardless of those values. Using `0` is unambiguous (not a real price). `stub://` makes the URI scheme distinguishable from real IPFS URIs.

### 5. `size_pct` for FLAT intents

**Decision**: `size_pct=Decimal("1")` for FLAT (100% of the position to close).

**Rationale**: FLAT means exit the full position. The trading engine interprets `size_pct` relative to the open position size for exits.

### 6. Factory raises on unknown `implementation`

**Decision**: `make_brain(config)` raises `ConfigError` for unrecognised implementation strings.

**Rationale**: A silent fallback (e.g., to stub) could mask misconfiguration in production. Fail fast and clearly. `deterministic_llm` is registered in the factory even though `brain/deterministic_llm.py` doesn't exist yet — it raises `ImportError` with a helpful message until Task 15 implements it.

## Risks / Trade-offs

- **`PriceFeedData` field names must match MCP server output** — if the MCP service returns `rsi_14` but the model expects `rsi14`, the integration will silently use defaults. Mitigation: field names are taken directly from `compute_indicators()` in `price_feed_mcp/service/indicators.py`.
- **StubBrain uses string Decimals from signal data** — all price/indicator fields in `PriceFeedData` are `str` (matching MCP JSON output). The brain converts via `Decimal(field)`. If a field is missing or `None`, the default score contribution is `0.0`. This is safe for a stub but must be documented.
- **`OnchainData` netflow fields** — the current `OnchainData` model doesn't have a `netflow_direction` field. The stub will infer netflow score from `funding_rate` sign as a proxy until onchain-data-mcp adds netflow (Task 5 outcome). Note this as a limitation.

## Migration Plan

No migration needed — the brain module is entirely new and the `SignalBundle` additions are additive (new optional fields + filling in previously-empty stubs). Existing tests for `SignalBundle` round-trip serialization remain valid.
