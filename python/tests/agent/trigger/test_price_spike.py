from decimal import Decimal
from unittest.mock import AsyncMock, MagicMock

import pytest
import respx
from httpx import Response

from agent.infra.price_feed_mcp import PriceFeedMCPClient
from agent.trigger.price_spike import PriceSpikeLoop

_BASE = "http://price-feed:8001"
_THRESHOLD = 3.0
_TRACKED = ["BTC", "ETH", "SOL"]


def _mcp_ok(result):
    return Response(200, json={"jsonrpc": "2.0", "id": "1", "result": result})


def _make_loop(
    changes: dict[str, dict[str, str]] | None = None,
    gate_open: dict[str, bool] | None = None,
) -> tuple[PriceSpikeLoop, AsyncMock, MagicMock]:
    """Build a PriceSpikeLoop with mocked price_feed, gate, and clock."""
    price_feed = AsyncMock()
    price_feed.get_prices_change_only.return_value = {
        asset: {window: Decimal(val) for window, val in windows.items()}
        for asset, windows in (changes or {}).items()
    }

    gate = MagicMock()
    gate.is_cooldown_open.side_effect = lambda a: (gate_open or {}).get(a, True)

    clock = AsyncMock()
    clock._run_once = AsyncMock()

    config = MagicMock()
    config.trigger.price_spike_threshold_pct = _THRESHOLD
    config.assets.tracked = _TRACKED

    loop = PriceSpikeLoop(price_feed=price_feed, gate=gate, clock=clock, config=config)
    return loop, clock, gate


# ── spike detection ────────────────────────────────────────────────────────────

@pytest.mark.parametrize(
    "changes, expect_brain",
    [
        # spike on 1m only
        ({"BTC": {"1m": "4.0", "5m": "0.5"}}, True),
        # spike on 5m only
        ({"BTC": {"1m": "1.0", "5m": "3.5"}}, True),
        # spike on both windows
        ({"BTC": {"1m": "4.0", "5m": "4.0"}}, True),
        # no spike — both below threshold
        ({"BTC": {"1m": "1.0", "5m": "2.9"}}, False),
        # negative spike on 1m (down move)
        ({"BTC": {"1m": "-4.0", "5m": "0.5"}}, True),
        # negative spike on 5m (down move)
        ({"BTC": {"1m": "0.0", "5m": "-3.1"}}, True),
        # exactly at threshold — triggers
        ({"BTC": {"1m": "3.0", "5m": "0.0"}}, True),
        # just below threshold — no trigger
        ({"BTC": {"1m": "2.99", "5m": "2.99"}}, False),
    ],
)
async def test_spike_detection(
    changes: dict[str, dict[str, str]],
    expect_brain: bool,
) -> None:
    loop, clock, _ = _make_loop(changes=changes)
    await loop._check()

    if expect_brain:
        clock._run_once.assert_awaited_once()
    else:
        clock._run_once.assert_not_awaited()


# ── cooldown gate suppression ──────────────────────────────────────────────────

async def test_spiking_asset_gated_skips_brain() -> None:
    changes = {"BTC": {"1m": "5.0", "5m": "0.0"}}
    loop, clock, _ = _make_loop(changes=changes, gate_open={"BTC": False})

    await loop._check()

    clock._run_once.assert_not_awaited()


async def test_spiking_asset_not_gated_triggers_brain() -> None:
    changes = {"BTC": {"1m": "5.0", "5m": "0.0"}}
    loop, clock, _ = _make_loop(changes=changes, gate_open={"BTC": True})

    await loop._check()

    clock._run_once.assert_awaited_once()


async def test_all_spiking_assets_gated_no_brain() -> None:
    changes = {
        "BTC": {"1m": "5.0", "5m": "0.0"},
        "ETH": {"1m": "4.5", "5m": "0.0"},
    }
    loop, clock, _ = _make_loop(
        changes=changes, gate_open={"BTC": False, "ETH": False}
    )

    await loop._check()

    clock._run_once.assert_not_awaited()


# ── multi-asset simultaneous spike ────────────────────────────────────────────

async def test_multi_asset_spike_fires_brain_once() -> None:
    changes = {
        "BTC": {"1m": "5.0", "5m": "0.0"},
        "ETH": {"1m": "4.0", "5m": "3.5"},
        "SOL": {"1m": "6.0", "5m": "5.0"},
    }
    loop, clock, _ = _make_loop(
        changes=changes, gate_open={"BTC": True, "ETH": True, "SOL": True}
    )

    await loop._check()

    # Brain triggered exactly once regardless of spike count
    clock._run_once.assert_awaited_once()


async def test_mixed_gated_ungated_fires_brain_once() -> None:
    changes = {
        "BTC": {"1m": "5.0", "5m": "0.0"},
        "ETH": {"1m": "4.0", "5m": "0.0"},
    }
    loop, clock, _ = _make_loop(
        changes=changes, gate_open={"BTC": False, "ETH": True}
    )

    await loop._check()

    # ETH passes gate → brain fires once
    clock._run_once.assert_awaited_once()


# ── error resilience ───────────────────────────────────────────────────────────

async def test_price_fetch_error_skips_cycle() -> None:
    loop, clock, _ = _make_loop()
    loop._price_feed.get_prices_change_only.side_effect = RuntimeError("timeout")

    await loop._check()  # must not raise

    clock._run_once.assert_not_awaited()


# ── get_prices_change_only unit test ──────────────────────────────────────────

@respx.mock
async def test_get_prices_change_only_single_rpc_call() -> None:
    """Verify a single get_prices_change RPC is made and values parsed as Decimal."""
    respx.post(f"{_BASE}/mcp").mock(return_value=_mcp_ok({
        "BTC": {"1m": "2.5", "5m": "-1.2", "1h": "3.0", "4h": "0.5", "24h": "-0.8"},
    }))

    client = PriceFeedMCPClient(_BASE)
    result = await client.get_prices_change_only(["BTC"])

    assert "BTC" in result
    assert result["BTC"]["1m"] == Decimal("2.5")
    assert result["BTC"]["5m"] == Decimal("-1.2")
    # Exactly 1 HTTP POST made
    assert respx.calls.call_count == 1


@respx.mock
async def test_get_prices_change_only_empty_returns_empty() -> None:
    client = PriceFeedMCPClient(_BASE)
    result = await client.get_prices_change_only([])
    assert result == {}
