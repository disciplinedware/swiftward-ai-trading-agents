## MODIFIED Requirements

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
