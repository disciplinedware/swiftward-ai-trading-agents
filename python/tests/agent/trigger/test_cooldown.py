from datetime import datetime, timezone
from unittest.mock import AsyncMock

import pytest
import time_machine

from agent.trigger.cooldown import CooldownGate
from common.config import TradingConfig, TriggerConfig
from common.models.portfolio_snapshot import PortfolioSnapshot


def _trigger(cooldown_minutes: int = 30) -> TriggerConfig:
    return TriggerConfig(
        cooldown_minutes=cooldown_minutes,
        price_spike_threshold_pct=3.0,
    )


def _trading(max_concurrent_positions: int = 2) -> TradingConfig:
    return TradingConfig(
        mode="paper",
        starting_balance_usdc=10000.0,
        max_concurrent_positions=max_concurrent_positions,
        database_url="sqlite+aiosqlite:///:memory:",
    )


def _portfolio(open_position_count: int) -> PortfolioSnapshot:
    return PortfolioSnapshot(
        total_usd="10000",
        stablecoin_balance="10000",
        open_position_count=open_position_count,
        realized_pnl_today="0",
        current_drawdown_pct="0",
    )


def _gate(
    cooldown_minutes: int = 30, max_positions: int = 2, open_positions: int = 0
) -> tuple[CooldownGate, AsyncMock]:
    mock_client = AsyncMock()
    mock_client.get_portfolio.return_value = _portfolio(open_positions)
    gate = CooldownGate(_trigger(cooldown_minutes), _trading(max_positions), mock_client)
    return gate, mock_client


@pytest.mark.parametrize("name,setup,asset,expected", [
    (
        "no history → allowed",
        lambda g, _: None,
        "BTC",
        True,
    ),
    (
        "record_trade closes gate immediately",
        lambda g, _: g.record_trade("BTC"),
        "BTC",
        False,
    ),
    (
        "per-asset isolation: BTC closed, ETH open",
        lambda g, _: g.record_trade("BTC"),
        "ETH",
        True,
    ),
    (
        "at max positions → suppressed (no cooldown)",
        lambda g, _: None,
        "ETH",
        False,  # max_positions=0 in the fixture below
    ),
])
async def test_is_allowed_scenarios(name, setup, asset, expected):
    # For "at max positions" case, override open_positions
    open_pos = 2 if name == "at max positions → suppressed (no cooldown)" else 0
    gate, _ = _gate(open_positions=open_pos)
    setup(gate, None)
    result = await gate.is_allowed(asset)
    assert result == expected, name


async def test_expiry_after_cooldown_window():
    gate, _ = _gate(cooldown_minutes=30)
    start = datetime(2026, 1, 1, 12, 0, 0, tzinfo=timezone.utc)
    with time_machine.travel(start, tick=False):
        gate.record_trade("BTC")
        assert await gate.is_allowed("BTC") is False

    # Advance past cooldown
    with time_machine.travel(start.replace(minute=31), tick=False):
        assert await gate.is_allowed("BTC") is True


async def test_mcp_error_suppresses_trade():
    mock_client = AsyncMock()
    mock_client.get_portfolio.side_effect = Exception("connection refused")
    gate = CooldownGate(_trigger(), _trading(), mock_client)
    # Should return False, not raise
    result = await gate.is_allowed("BTC")
    assert result is False


async def test_cooldown_short_circuits_mcp_call():
    gate, mock_client = _gate()
    gate.record_trade("BTC")
    await gate.is_allowed("BTC")
    mock_client.get_portfolio.assert_not_awaited()
