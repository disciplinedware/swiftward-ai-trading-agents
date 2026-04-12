"""Tests for the OnchainData Pydantic model."""

from common.models.signal_bundle import OnchainData


def test_onchain_data_empty_construction():
    data = OnchainData()
    assert data.funding_rate is None
    assert data.annualized_funding_pct is None
    assert data.next_funding_time is None
    assert data.oi_usd is None
    assert data.oi_change_pct_24h is None
    assert data.liquidated_usd_15m is None
    assert data.long_liquidated_usd is None
    assert data.short_liquidated_usd is None


def test_onchain_data_full_round_trip():
    data = OnchainData(
        funding_rate="-0.000300",
        annualized_funding_pct="-32.85",
        next_funding_time="2026-03-20T08:00:00Z",
        oi_usd="12345678900",
        oi_change_pct_24h="-3.42",
        liquidated_usd_15m="4500000",
        long_liquidated_usd="3200000",
        short_liquidated_usd="1300000",
    )
    dumped = data.model_dump()
    restored = OnchainData.model_validate(dumped)

    assert restored.funding_rate == "-0.000300"
    assert restored.annualized_funding_pct == "-32.85"
    assert restored.oi_change_pct_24h == "-3.42"
    assert restored == data
