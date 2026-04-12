"""Tests for LiveEngine — unconfigured router, EIP-712 constants, timeout."""
import asyncio
from decimal import Decimal
from unittest.mock import AsyncMock, MagicMock, patch

import pytest

from common.exceptions import MCPError
from common.models.trade_intent import TradeIntent
from trading_mcp.engine.live import (
    _DOMAIN_NAME,
    _DOMAIN_VERSION,
    _TRADE_INTENT_TYPES,
    LiveEngine,
)

_INTENT = TradeIntent(
    asset="ETH",
    action="LONG",
    size_pct=Decimal("0.1"),
    stop_loss=Decimal("2800"),
    take_profit=Decimal("3500"),
    strategy="trend_following",
    reasoning="mock://reasoning.json",
    trigger_reason="clock",
    confidence=0.8,
)


def _make_engine(router_address: str = "0xRouter123") -> LiveEngine:
    return LiveEngine(
        risk_router_address=router_address,
        chain_id=11155111,
        agent_id=42,
        wallet_address="0x" + "ab" * 20,
        wallet_private_key="0xdeadbeef",
        rpc_url="https://rpc.example.com",
        tx_lock=asyncio.Lock(),
    )


@pytest.mark.parametrize(
    "name,router_address",
    [
        ("empty string", ""),
        ("placeholder dots", "0x..."),
        ("zero address", "0x0000000000000000000000000000000000000000"),
    ],
)
async def test_unconfigured_router_raises_runtime_error(name, router_address):
    engine = _make_engine(router_address=router_address)
    with pytest.raises(MCPError):
        await engine.execute_swap(_INTENT, Decimal("3100"), Decimal("1000"))


def test_eip712_domain_matches_contract():
    """Domain name and version must match RiskRouter.sol constructor: EIP712("RiskRouter", "1")."""
    assert _DOMAIN_NAME == "RiskRouter"
    assert _DOMAIN_VERSION == "1"


def test_eip712_struct_fields_match_solidity():
    """TradeIntent struct field names and order must match the Solidity struct exactly."""
    fields = [f["name"] for f in _TRADE_INTENT_TYPES["TradeIntent"]]
    assert fields == [
        "agentId", "agentWallet", "pair", "action",
        "amountUsdScaled", "maxSlippageBps", "nonce", "deadline",
    ]


async def test_confirmation_timeout_raises_timeout_error():
    """When wait_for_transaction_receipt times out, execute_swap raises TimeoutError."""
    engine = _make_engine(router_address="0x" + "ab" * 20)

    mock_nonce_fn = MagicMock()
    mock_nonce_fn.call = AsyncMock(return_value=0)

    mock_submit_fn = MagicMock()
    mock_submit_fn.build_transaction = AsyncMock(return_value={
        "from": "0x" + "ab" * 20, "nonce": 0, "gasPrice": 1_000_000_000,
        "to": "0xRealRouter", "data": b"", "value": 0,
    })

    mock_contract = MagicMock()
    mock_contract.functions.getIntentNonce.return_value = mock_nonce_fn
    mock_contract.functions.submitTradeIntent.return_value = mock_submit_fn

    mock_signed = MagicMock()
    mock_signed.signature = b"\x00" * 65
    mock_signed_tx = MagicMock()
    mock_signed_tx.raw_transaction = b"\x00" * 32

    async def _val(v):
        return v

    mock_w3 = MagicMock()
    mock_w3.to_checksum_address.return_value = "0x" + "ab" * 20
    mock_w3.eth.account.sign_typed_data.return_value = mock_signed
    mock_w3.eth.account.sign_transaction.return_value = mock_signed_tx
    mock_w3.eth.gas_price = _val(1_000_000_000)
    mock_w3.eth.get_transaction_count = AsyncMock(return_value=0)
    mock_w3.eth.send_raw_transaction = AsyncMock(return_value=b"\xab" * 32)

    engine._w3 = mock_w3
    engine._contract = mock_contract

    with patch("asyncio.wait_for", side_effect=asyncio.TimeoutError):
        with pytest.raises(MCPError):
            await engine.execute_swap(_INTENT, Decimal("3100"), Decimal("1000"))
