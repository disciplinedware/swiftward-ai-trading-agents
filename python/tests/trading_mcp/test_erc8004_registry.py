"""Tests for ERC8004Registry — identity registration (raw web3, no SDK)."""
import asyncio
from datetime import datetime, timezone
from unittest.mock import AsyncMock, MagicMock, patch

import pytest
from sqlalchemy import event, select

from trading_mcp.domain.entity import Agent, Base
from trading_mcp.erc8004.ipfs import MockIpfs
from trading_mcp.erc8004.registry import ERC8004Config, ERC8004Registry
from trading_mcp.infra.db import make_engine, make_session_factory

# ---------------------------------------------------------------------------
# DB fixture
# ---------------------------------------------------------------------------


@pytest.fixture
async def db_factory():
    engine = make_engine("sqlite+aiosqlite:///:memory:")

    @event.listens_for(engine.sync_engine, "connect")
    def set_pragma(dbapi_conn, _):
        cursor = dbapi_conn.cursor()
        cursor.execute("PRAGMA foreign_keys=ON")
        cursor.close()

    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    factory = make_session_factory(engine)
    yield factory
    await engine.dispose()


def _make_config() -> ERC8004Config:
    return ERC8004Config(
        chain_id=11155111,
        rpc_url="http://localhost:8545",
        identity_registry_address="0x0000000000000000000000000000000000000001",
        validation_registry_address="0x0000000000000000000000000000000000000002",
        wallet_address="0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
        wallet_private_key="0x" + "ab" * 32,
        tracked_assets=["BTC", "ETH"],
        ipfs_provider="mock",
        ipfs_api_key="",
    )


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_registry(db_factory, ipfs=None) -> ERC8004Registry:
    """Construct ERC8004Registry with web3 patched out."""
    if ipfs is None:
        ipfs = MockIpfs()
    with patch("trading_mcp.erc8004.registry.AsyncWeb3"):
        return ERC8004Registry(_make_config(), ipfs, db_factory, asyncio.Lock())


# ---------------------------------------------------------------------------
# register_identity
# ---------------------------------------------------------------------------


async def test_register_identity_returns_existing_when_db_has_agent(db_factory):
    """If an Agent row already exists, register_identity returns its agentId immediately."""
    async with db_factory() as session:
        async with session.begin():
            session.add(
                Agent(
                    agent_id=42,
                    wallet_address="0xabc",
                    registration_uri="mock:///existing",
                    registered_at=datetime.now(tz=timezone.utc),
                )
            )

    registry = _make_registry(db_factory)
    result = await registry.register_identity()
    assert result == 42


async def test_register_identity_syncs_from_chain_when_wallet_registered(db_factory):
    """If wallet is registered on-chain but not in DB, sync to DB."""
    registry = _make_registry(db_factory)

    # Mock on-chain walletToAgentId → returns 7
    registry._identity_contract = MagicMock()
    wallet_fn = AsyncMock(return_value=7)
    registry._identity_contract.functions.walletToAgentId.return_value.call = wallet_fn

    # Mock getAgent → returns tuple with registeredAt at index 5
    ts = int(datetime.now(tz=timezone.utc).timestamp())
    agent_info = ("0xop", "0xag", "AI Trading Agent", "desc", ["cap"], ts, True)
    get_fn = AsyncMock(return_value=agent_info)
    registry._identity_contract.functions.getAgent.return_value.call = get_fn

    result = await registry.register_identity()
    assert result == 7

    async with db_factory() as session:
        agent = await session.scalar(select(Agent))
    assert agent is not None
    assert agent.agent_id == 7


async def test_register_identity_registers_new_agent(db_factory):
    """With no existing agent, uploads metadata to IPFS, registers on-chain, saves to DB."""
    registry = _make_registry(db_factory)

    # walletToAgentId → 0 (not registered)
    registry._identity_contract = MagicMock()
    registry._identity_contract.functions.walletToAgentId.return_value.call = AsyncMock(
        return_value=0
    )

    # Mock _send_tx → returns a receipt
    mock_receipt = MagicMock()
    registry._send_tx = AsyncMock(return_value=mock_receipt)

    # Mock event parsing → agentId=99
    registry._parse_agent_registered = MagicMock(return_value=99)

    result = await registry.register_identity()
    assert result == 99

    registry._send_tx.assert_awaited_once()

    async with db_factory() as session:
        agent = await session.scalar(select(Agent))
    assert agent is not None
    assert agent.agent_id == 99
    assert "mock://" in agent.registration_uri
