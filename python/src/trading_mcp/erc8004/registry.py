"""ERC-8004 registry hooks — identity and validation.

All public methods are fire-and-forget: call them with asyncio.create_task().
They never raise to the caller; errors are logged internally.

Identity uses the AgentRegistry contract (ERC-721 based).
Validation uses the ValidationRegistry contract (attestation-based).
Both use raw web3.py — no SDK dependency.
"""
import asyncio
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

from eth_account import Account
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker
from web3 import AsyncWeb3

from common.log import get_logger
from trading_mcp.domain.entity.agent import Agent
from trading_mcp.domain.entity.position import Position
from trading_mcp.erc8004.checkpoint import (
    build_checkpoint_value,
    build_domain,
    sign_checkpoint,
)
from trading_mcp.erc8004.ipfs import IpfsProvider

logger = get_logger(__name__)

_ABI_DIR = Path(__file__).parent / "abis"


@dataclass
class ERC8004Config:
    chain_id: int
    rpc_url: str
    identity_registry_address: str
    validation_registry_address: str
    wallet_address: str  # agent wallet used as self-validator (checksummed hex)
    wallet_private_key: str  # hex private key for signing transactions (0x-prefixed)
    tracked_assets: list[str]
    ipfs_provider: str  # "pinata" | "mock"
    ipfs_api_key: str  # pinata JWT


class ERC8004Registry:
    """Async ERC-8004 registry hooks.

    Usage (fire-and-forget):
        asyncio.create_task(registry.register_identity())
        asyncio.create_task(registry.submit_validation(position_id))
    """

    def __init__(
        self,
        config: ERC8004Config,
        ipfs: IpfsProvider,
        session_factory: async_sessionmaker[AsyncSession],
        tx_lock: asyncio.Lock,
    ) -> None:
        self._cfg = config
        self._ipfs = ipfs
        self._sf = session_factory
        self._w3: AsyncWeb3 = AsyncWeb3(AsyncWeb3.AsyncHTTPProvider(config.rpc_url))
        self._tx_lock = tx_lock
        self._account: Account = Account.from_key(config.wallet_private_key)

        self._identity_contract = self._w3.eth.contract(
            address=self._w3.to_checksum_address(config.identity_registry_address),
            abi=json.loads((_ABI_DIR / "agent_registry.json").read_text()),
        )
        self._validation_contract = self._w3.eth.contract(
            address=self._w3.to_checksum_address(config.validation_registry_address),
            abi=json.loads((_ABI_DIR / "validation_registry.json").read_text()),
        )

    # ------------------------------------------------------------------
    # Public hooks
    # ------------------------------------------------------------------

    async def register_identity(self) -> int | None:
        """Register agent identity on-chain via AgentRegistry (ERC-721).

        Returns the agentId (int) on success, or None on failure.
        Skipped if an Agent row already exists in the DB — returns existing agentId.
        """
        try:
            # 1. Check DB for existing registration
            async with self._sf() as session:
                existing = await session.scalar(select(Agent).limit(1))

            if existing is not None:
                logger.info(
                    "ERC-8004 identity already registered (DB)",
                    agent_id=existing.agent_id,
                )
                return existing.agent_id

            # 2. Check on-chain — wallet may already be registered from a previous run
            wallet = self._w3.to_checksum_address(self._cfg.wallet_address)
            on_chain_id = await self._identity_contract.functions.walletToAgentId(wallet).call()

            if on_chain_id > 0:
                # Already on-chain but not in DB — persist and return
                agent_info = await self._identity_contract.functions.getAgent(on_chain_id).call()
                registered_at = datetime.fromtimestamp(agent_info[5], tz=timezone.utc)
                async with self._sf() as session:
                    async with session.begin():
                        session.add(
                            Agent(
                                agent_id=on_chain_id,
                                wallet_address=self._cfg.wallet_address,
                                registration_uri="",
                                registered_at=registered_at,
                            )
                        )
                logger.info(
                    "ERC-8004 identity found on-chain, synced to DB",
                    agent_id=on_chain_id,
                )
                return on_chain_id

            # 3. Upload agent metadata to IPFS
            metadata = {
                "name": "AI Trading Agent",
                "description": "Autonomous crypto trading agent. Strategy: long-only spot.",
            }
            agent_uri = await self._ipfs.upload(
                metadata,
                f"agent_identity_{self._cfg.chain_id}_{self._cfg.wallet_address}.json",
            )

            # 4. Register on-chain — mints ERC-721 token
            receipt = await self._send_tx(
                self._identity_contract.functions.register(
                    wallet,
                    "AI Trading Agent",
                    "Autonomous crypto trading agent. Strategy: long-only spot.",
                    ["finance/trading", "long-only-spot"],
                    agent_uri,
                )
            )

            # 5. Parse AgentRegistered event for agentId
            agent_id = self._parse_agent_registered(receipt)
            if agent_id is None:
                raise RuntimeError("Could not parse AgentRegistered event from tx receipt")

            # 6. Persist to DB
            async with self._sf() as session:
                async with session.begin():
                    session.add(
                        Agent(
                            agent_id=agent_id,
                            wallet_address=self._cfg.wallet_address,
                            registration_uri=agent_uri,
                            registered_at=datetime.now(tz=timezone.utc),
                        )
                    )
            logger.info("ERC-8004 identity registered", agent_id=agent_id, uri=agent_uri)
            return agent_id
        except Exception:
            logger.exception("ERC-8004 register_identity failed")
            return None

    async def submit_validation(
        self, position_id: int, intent_hash: bytes, confidence: float,
    ) -> None:
        """Build an EIP-712 checkpoint for a position and post attestation on-chain."""
        async with self._tx_lock:
            try:
                await self._submit_validation(position_id, intent_hash, confidence)
            except Exception:
                logger.exception(
                    "ERC-8004 submit_validation failed", position_id=position_id,
                )

    async def _submit_validation(
        self, position_id: int, intent_hash: bytes, confidence: float,
    ) -> None:
        if self._cfg.validation_registry_address in ("0x", "0x..."):
            logger.debug("submit_validation: skipped — validation_registry_address not configured")
            return

        async with self._sf() as session:
            pos = await session.get(Position, position_id)
            agent_row = await session.scalar(select(Agent).limit(1))
        if pos is None:
            logger.warning("submit_validation: position not found", position_id=position_id)
            return
        if agent_row is None:
            logger.warning("submit_validation: no agentId — identity not registered")
            return

        # 1. Build and sign EIP-712 checkpoint
        value = build_checkpoint_value(
            agent_id=agent_row.agent_id,
            action=pos.action,
            asset=pos.asset,
            amount_usd=float(pos.size_usd),
            price_usd=float(pos.entry_price),
            reasoning=pos.reasoning,
            intent_hash=intent_hash,
            confidence=confidence,
        )
        domain = build_domain(
            self._cfg.identity_registry_address,
            self._cfg.chain_id,
        )
        _signature, checkpoint_hash = sign_checkpoint(
            value, domain, self._cfg.wallet_private_key,
        )

        # 2. Post EIP-712 attestation on-chain
        score = round(confidence * 100)  # 0.0–1.0 → 0–100
        notes = f"asset={pos.asset} action={pos.action} strategy={pos.strategy}"
        await self._send_tx(
            self._validation_contract.functions.postEIP712Attestation(
                agent_row.agent_id,
                checkpoint_hash,
                score,
                notes,
            )
        )

        logger.info(
            "ERC-8004 validation submitted",
            position_id=position_id,
            checkpoint_hash=checkpoint_hash.hex(),
        )

    # ------------------------------------------------------------------
    # Private
    # ------------------------------------------------------------------

    def _parse_agent_registered(self, receipt) -> int | None:
        """Extract agentId from the AgentRegistered event in a tx receipt."""
        try:
            logs = self._identity_contract.events.AgentRegistered().process_receipt(receipt)
            if logs:
                return int(logs[0]["args"]["agentId"])
        except Exception:
            logger.exception("Failed to parse AgentRegistered event")
        return None

    async def _send_tx(self, fn):
        """Build, sign, send a contract function call. Returns the tx receipt."""
        w3 = self._w3
        addr = w3.to_checksum_address(self._cfg.wallet_address)
        nonce = await w3.eth.get_transaction_count(addr, "pending")
        gas_price = await w3.eth.gas_price
        tx = await fn.build_transaction(
            {
                "from": addr,
                "nonce": nonce,
                "gasPrice": gas_price,
            }
        )
        signed = w3.eth.account.sign_transaction(tx, self._cfg.wallet_private_key)
        tx_hash = await w3.eth.send_raw_transaction(signed.raw_transaction)
        return await w3.eth.wait_for_transaction_receipt(tx_hash)
