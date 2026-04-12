## Why

The agent pipeline (Tasks 13–20) cannot be wired or tested without a concrete `Brain` implementation. Task 12 establishes the pluggable brain interface, a factory that selects the implementation from config, and a fully deterministic stub brain — no LLM, no external calls — so all downstream loops can be developed and tested against a real (if simple) brain.

## What Changes

- **New**: `Brain` protocol in `src/agent/brain/base.py` — single async method `run(signal_bundle) -> list[TradeIntent]`
- **New**: `make_brain(config) -> Brain` factory in `src/agent/brain/factory.py` — selects implementation from `config.brain.implementation`
- **New**: `StubBrain` in `src/agent/brain/stub.py` — deterministic 3-stage pipeline (market filter, rotation selector, decision engine), no LLM
- **New**: `PortfolioSnapshot` Pydantic model in `src/common/models/portfolio_snapshot.py` — common representation of portfolio state for use in SignalBundle
- **Modified**: `SignalBundle` in `src/common/models/signal_bundle.py` — fill in `PriceFeedData`, `FearGreedData`, `NewsData` stub fields; add `portfolio: PortfolioSnapshot` field
- **Modified**: `BrainConfig` in `src/common/config.py` — add `implementation: str` field
- **Modified**: `config/config.example.yaml` — add `brain.implementation: stub`

## Capabilities

### New Capabilities

- `brain-protocol`: The `Brain` async protocol and `make_brain` factory — the pluggable interface all brain implementations must satisfy
- `stub-brain`: Deterministic 3-stage brain (market filter → rotation selector → decision engine) with no LLM dependency; used in backtesting and initial pipeline wiring
- `portfolio-snapshot`: Common `PortfolioSnapshot` Pydantic model in `common/models/` representing portfolio state passed into SignalBundle; fills the gap between trading-mcp's DTO and the agent's signal models
- `signal-bundle-fields`: Concrete fields for `PriceFeedData`, `FearGreedData`, `NewsData` matching what MCP servers actually return — unblocks the brain's deterministic score computation

### Modified Capabilities

- `signal-bundle`: `SignalBundle` gains a `portfolio` field and all sub-model stubs are filled in — a behavioral change to the shared model contract

## Impact

- `src/common/models/signal_bundle.py` — extended (breaking: sub-model fields added, portfolio field added)
- `src/common/models/portfolio_snapshot.py` — new file
- `src/common/config.py` — `BrainConfig` extended with `implementation`
- `config/config.example.yaml` — new key added
- `src/agent/brain/` — new directory with 3 files
- `tests/agent/brain/test_stub.py` — new test file
- No changes to MCP servers or trading engine
