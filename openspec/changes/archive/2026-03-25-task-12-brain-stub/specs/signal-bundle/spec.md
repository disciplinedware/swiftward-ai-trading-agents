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
