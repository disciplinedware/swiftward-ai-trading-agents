## ADDED Requirements

### Requirement: SignalBundle model
The system SHALL provide a `SignalBundle` Pydantic `BaseModel` at `src/common/models/signal_bundle.py` aggregating inputs to the agent brain from all MCP input servers.

Structure:
- `prices: dict[str, PriceFeedData]` — asset symbol → price feed data (stub)
- `fear_greed: FearGreedData` — single global object (stub)
- `onchain: dict[str, OnchainData]` — asset symbol → on-chain data
- `news: dict[str, NewsData]` — asset symbol → news/sentiment data (stub)

`OnchainData` SHALL be a Pydantic `BaseModel` with the following optional fields (all `str | None`, defaulting to `None` for backward compatibility):
- `funding_rate: str | None` — current funding rate (can be negative)
- `annualized_funding_pct: str | None` — annualized funding rate %
- `next_funding_time: str | None` — ISO 8601 UTC timestamp
- `oi_usd: str | None` — open interest in USD
- `oi_change_pct_24h: str | None` — OI % change over 24h
- `liquidated_usd_15m: str | None` — USD liquidated in last 15 min
- `long_liquidated_usd: str | None` — long liquidations USD
- `short_liquidated_usd: str | None` — short liquidations USD

Sub-classes `PriceFeedData`, `FearGreedData`, `NewsData` remain stubs with no fields until their respective MCP tasks fill them in.

#### Scenario: Empty bundle round-trip
- **WHEN** a `SignalBundle` is created with empty dicts and a bare `FearGreedData()` and serialized via `model_dump()`
- **THEN** `model_validate()` of the result returns an equal `SignalBundle`

#### Scenario: Bundle with stub sub-objects
- **WHEN** `SignalBundle` is constructed with `prices={"BTC": PriceFeedData()}` and other fields populated
- **THEN** `bundle.prices["BTC"]` is an instance of `PriceFeedData`

#### Scenario: OnchainData with funding fields
- **WHEN** `OnchainData(funding_rate="-0.000300", annualized_funding_pct="-32.85")` is created
- **THEN** the fields are accessible and `model_dump()` round-trips correctly

#### Scenario: OnchainData empty (backward compatibility)
- **WHEN** `OnchainData()` is created with no arguments
- **THEN** all fields are `None` and no validation error is raised

### Requirement: SignalBundle sub-class stubs are importable
The sub-classes `PriceFeedData`, `FearGreedData`, `OnchainData`, `NewsData` SHALL be importable from `common.models` so MCP tasks can extend them without changing the import path.

#### Scenario: Import sub-classes from common.models
- **WHEN** code does `from common.models import PriceFeedData, FearGreedData, OnchainData, NewsData`
- **THEN** no `ImportError` is raised

## MODIFIED Requirements

### Requirement: SignalBundle includes portfolio state
`SignalBundle` in `src/common/models/signal_bundle.py` SHALL include a `portfolio` field of type `PortfolioSnapshot` (imported from `common.models.portfolio_snapshot`). The field SHALL have a default of `PortfolioSnapshot(total_usd="0", stablecoin_balance="0", open_position_count=0, realized_pnl_today="0", current_drawdown_pct="0")` so existing usages that don't supply portfolio data remain valid.

`SignalBundle` fields after this change:
- `prices: dict[str, PriceFeedData]`
- `fear_greed: FearGreedData`
- `onchain: dict[str, OnchainData]`
- `news: dict[str, NewsData]`
- `portfolio: PortfolioSnapshot`

#### Scenario: SignalBundle constructed without portfolio field
- **WHEN** `SignalBundle` is constructed without supplying `portfolio`
- **THEN** `signal_bundle.portfolio.open_position_count` SHALL equal `0` and `signal_bundle.portfolio.open_positions` SHALL be empty

#### Scenario: SignalBundle round-trip serialization with portfolio
- **WHEN** a `SignalBundle` with a non-empty `portfolio` is serialized and re-validated
- **THEN** `open_positions` SHALL contain the same entries as before serialization

#### Scenario: Brain receives portfolio via signal bundle
- **WHEN** the clock loop passes a `SignalBundle` with `portfolio.open_positions = [OpenPositionView(asset="SOL", ...)]`
- **THEN** `StubBrain.run()` SHALL apply the held-asset bonus to SOL in Stage 2 ranking
