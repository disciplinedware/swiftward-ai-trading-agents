from datetime import datetime, timezone
from decimal import Decimal

import pytest
from pydantic import ValidationError

from common.models import (
    FearGreedData,
    NewsData,
    OnchainData,
    OpenPositionView,
    PortfolioSnapshot,
    Position,
    PriceFeedData,
    SignalBundle,
    TradeIntent,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

NOW = datetime(2026, 3, 18, 12, 0, 0, tzinfo=timezone.utc)

VALID_INTENT = dict(
    asset="SOL",
    action="LONG",
    size_pct="0.09",
    stop_loss="142.30",
    take_profit="156.80",
    strategy="trend_following",
    reasoning="ipfs://Qm123",
    trigger_reason="clock",
    confidence=0.8,
)

VALID_OPEN_POSITION = dict(
    asset="SOL",
    status="open",
    action="LONG",
    entry_price="143.21",
    size_usd="900.00",
    size_pct="0.09",
    stop_loss="142.30",
    take_profit="156.80",
    strategy="trend_following",
    trigger_reason="clock",
    reasoning="ipfs://Qm123",
    opened_at=NOW,
    tx_hash_open="paper_abc123",
)

VALID_CLOSED_POSITION = dict(
    **{k: v for k, v in VALID_OPEN_POSITION.items() if k != "status"},
    status="closed",
    closed_at=NOW,
    exit_reason="take_profit",
    exit_price="156.80",
    realized_pnl_usd="136.50",
    realized_pnl_pct="0.1517",
    tx_hash_close="paper_xyz456",
)


# ---------------------------------------------------------------------------
# TradeIntent
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "name, overrides, expect_error",
    [
        ("valid long", {}, False),
        ("valid flat", {"action": "FLAT", "strategy": "mean_reversion"}, False),
        ("invalid action", {"action": "SHORT"}, True),
        ("invalid strategy", {"strategy": "scalping"}, True),
        ("invalid trigger_reason", {"trigger_reason": "unknown"}, True),
    ],
)
def test_trade_intent_validation(name, overrides, expect_error):
    data = {**VALID_INTENT, **overrides}
    if expect_error:
        with pytest.raises(ValidationError):
            TradeIntent.model_validate(data)
    else:
        intent = TradeIntent.model_validate(data)
        assert intent.asset == data["asset"]


def test_trade_intent_round_trip():
    intent = TradeIntent.model_validate(VALID_INTENT)
    dumped = intent.model_dump()

    # Decimal fields serialize to str
    assert isinstance(dumped["size_pct"], str)
    assert isinstance(dumped["stop_loss"], str)
    assert isinstance(dumped["take_profit"], str)

    # Round-trip
    restored = TradeIntent.model_validate(dumped)
    assert restored == intent


@pytest.mark.parametrize(
    "field, value",
    [
        ("stop_loss", "142.30"),
        ("stop_loss", 142.30),
        ("stop_loss", Decimal("142.30")),
        ("size_pct", "0.09"),
    ],
)
def test_trade_intent_decimal_accepts_multiple_types(field, value):
    data = {**VALID_INTENT, field: value}
    intent = TradeIntent.model_validate(data)
    assert isinstance(getattr(intent, field), Decimal)
    assert getattr(intent, field) == Decimal(str(value))


# ---------------------------------------------------------------------------
# Position
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "name, data",
    [
        ("open position", VALID_OPEN_POSITION),
        ("closed position", VALID_CLOSED_POSITION),
    ],
)
def test_position_round_trip(name, data):
    pos = Position.model_validate(data)
    dumped = pos.model_dump()
    restored = Position.model_validate(dumped)
    assert restored == pos


def test_open_position_closed_fields_are_none():
    pos = Position.model_validate(VALID_OPEN_POSITION)
    assert pos.closed_at is None
    assert pos.exit_reason is None
    assert pos.exit_price is None
    assert pos.realized_pnl_usd is None
    assert pos.realized_pnl_pct is None
    assert pos.tx_hash_close is None


def test_position_decimal_serialization():
    pos = Position.model_validate(VALID_OPEN_POSITION)
    dumped = pos.model_dump()
    for field in ("entry_price", "stop_loss", "take_profit", "size_usd", "size_pct"):
        assert isinstance(dumped[field], str), f"{field} should serialize to str"


def test_closed_position_decimal_fields():
    pos = Position.model_validate(VALID_CLOSED_POSITION)
    assert isinstance(pos.exit_price, Decimal)
    assert isinstance(pos.realized_pnl_usd, Decimal)
    assert isinstance(pos.realized_pnl_pct, Decimal)
    dumped = pos.model_dump()
    assert isinstance(dumped["exit_price"], str)
    assert isinstance(dumped["realized_pnl_usd"], str)


# ---------------------------------------------------------------------------
# SignalBundle
# ---------------------------------------------------------------------------


def _empty_portfolio() -> PortfolioSnapshot:
    return PortfolioSnapshot(
        total_usd="0",
        stablecoin_balance="0",
        open_position_count=0,
        realized_pnl_today="0",
        current_drawdown_pct="0",
    )


def test_signal_bundle_empty_round_trip():
    bundle = SignalBundle(
        prices={},
        fear_greed=FearGreedData(),
        onchain={},
        news={},
        portfolio=_empty_portfolio(),
        trigger_reason="clock",
    )
    dumped = bundle.model_dump()
    restored = SignalBundle.model_validate(dumped)
    assert restored == bundle


def test_signal_bundle_with_stub_sub_objects():
    bundle = SignalBundle(
        prices={"BTC": PriceFeedData(), "SOL": PriceFeedData()},
        fear_greed=FearGreedData(),
        onchain={"BTC": OnchainData()},
        news={"ETH": NewsData()},
        portfolio=_empty_portfolio(),
        trigger_reason="clock",
    )
    assert isinstance(bundle.prices["BTC"], PriceFeedData)
    assert isinstance(bundle.fear_greed, FearGreedData)
    assert isinstance(bundle.onchain["BTC"], OnchainData)
    assert isinstance(bundle.news["ETH"], NewsData)


def test_signal_bundle_sub_class_imports():
    # Verify all sub-classes are importable from common.models (tested by the import at top)
    assert PriceFeedData is not None
    assert FearGreedData is not None
    assert OnchainData is not None
    assert NewsData is not None


def test_signal_bundle_portfolio_fields():
    bundle = SignalBundle(
        prices={}, fear_greed=FearGreedData(), onchain={}, news={},
        portfolio=_empty_portfolio(), trigger_reason="clock",
    )
    assert bundle.portfolio.open_position_count == 0
    assert bundle.portfolio.open_positions == []


def test_signal_bundle_with_portfolio_round_trip():
    pos = OpenPositionView(
        asset="SOL",
        entry_price="143.00",
        stop_loss="140.00",
        take_profit="155.00",
        size_pct="0.09",
        strategy="trend_following",
    )
    portfolio = PortfolioSnapshot(
        total_usd="10000",
        stablecoin_balance="9100",
        open_position_count=1,
        realized_pnl_today="0",
        current_drawdown_pct="0",
        open_positions=[pos],
    )
    bundle = SignalBundle(
        prices={}, fear_greed=FearGreedData(), onchain={}, news={},
        portfolio=portfolio, trigger_reason="clock",
    )
    dumped = bundle.model_dump()
    restored = SignalBundle.model_validate(dumped)
    assert len(restored.portfolio.open_positions) == 1
    assert restored.portfolio.open_positions[0].asset == "SOL"


# ---------------------------------------------------------------------------
# PortfolioSnapshot
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "name, positions, expected_assets",
    [
        ("empty", [], set()),
        (
            "one position",
            [{"asset": "SOL", "entry_price": "143", "stop_loss": "140",
              "take_profit": "155", "size_pct": "0.09", "strategy": "trend_following"}],
            {"SOL"},
        ),
        (
            "two positions",
            [
                {"asset": "BTC", "entry_price": "60000", "stop_loss": "58000",
                 "take_profit": "65000", "size_pct": "0.09", "strategy": "trend_following"},
                {"asset": "ETH", "entry_price": "3000", "stop_loss": "2900",
                 "take_profit": "3300", "size_pct": "0.09", "strategy": "breakout"},
            ],
            {"BTC", "ETH"},
        ),
    ],
)
def test_portfolio_snapshot_held_assets(name, positions, expected_assets):
    snap = PortfolioSnapshot(
        total_usd="10000",
        stablecoin_balance="8200",
        open_position_count=len(positions),
        realized_pnl_today="0",
        current_drawdown_pct="0",
        open_positions=[OpenPositionView(**p) for p in positions],
    )
    assert {p.asset for p in snap.open_positions} == expected_assets


def test_portfolio_snapshot_round_trip():
    snap = PortfolioSnapshot(
        total_usd="10000.50",
        stablecoin_balance="9100",
        open_position_count=1,
        realized_pnl_today="123.45",
        current_drawdown_pct="0.05",
        open_positions=[
            OpenPositionView(
                asset="SOL",
                entry_price="143.21",
                stop_loss="140.00",
                take_profit="156.80",
                size_pct="0.09",
                strategy="trend_following",
            )
        ],
    )
    dumped = snap.model_dump()
    assert isinstance(dumped["total_usd"], str)
    restored = PortfolioSnapshot.model_validate(dumped)
    assert restored == snap
