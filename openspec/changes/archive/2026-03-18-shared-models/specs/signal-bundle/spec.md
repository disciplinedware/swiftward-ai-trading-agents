## ADDED Requirements

### Requirement: SignalBundle model
The system SHALL provide a `SignalBundle` Pydantic `BaseModel` at `src/common/models/signal_bundle.py` aggregating inputs to the agent brain from all MCP input servers.

Structure:
- `prices: dict[str, PriceFeedData]` — asset symbol → price feed data (stub)
- `fear_greed: FearGreedData` — single global object (stub)
- `onchain: dict[str, OnchainData]` — asset symbol → on-chain data (stub)
- `news: dict[str, NewsData]` — asset symbol → news/sentiment data (stub)

Sub-classes (`PriceFeedData`, `FearGreedData`, `OnchainData`, `NewsData`) SHALL be defined in the same file as Pydantic `BaseModel` stubs with no fields (bodies can be `pass`). Their fields are finalized during MCP implementation tasks (3–6).

#### Scenario: Empty bundle round-trip
- **WHEN** a `SignalBundle` is created with empty dicts and a bare `FearGreedData()` and serialized via `model_dump()`
- **THEN** `model_validate()` of the result returns an equal `SignalBundle`

#### Scenario: Bundle with stub sub-objects
- **WHEN** `SignalBundle` is constructed with `prices={"BTC": PriceFeedData()}` and other fields populated
- **THEN** `bundle.prices["BTC"]` is an instance of `PriceFeedData`

### Requirement: SignalBundle sub-class stubs are importable
The sub-classes `PriceFeedData`, `FearGreedData`, `OnchainData`, `NewsData` SHALL be importable from `common.models` so MCP tasks can extend them without changing the import path.

#### Scenario: Import sub-classes from common.models
- **WHEN** code does `from common.models import PriceFeedData, FearGreedData, OnchainData, NewsData`
- **THEN** no `ImportError` is raised
