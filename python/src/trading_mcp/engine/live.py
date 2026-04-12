"""Live execution engine — EIP-712 calldata construction + Risk Router submission.

Set `chain.risk_router_address` in config to the real Risk Router contract address.
Until then, all execute_swap calls raise RuntimeError.
"""
import asyncio
import json
from decimal import Decimal
from pathlib import Path
from time import time

from eth_abi.abi import encode
from eth_hash.auto import keccak
from web3 import AsyncWeb3
from web3.exceptions import TimeExhausted
from web3.types import Nonce, TxParams, Wei

from common.exceptions import MCPError
from common.log import get_logger
from common.models.trade_intent import TradeIntent
from trading_mcp.engine.interface import Engine, ExecutionResult

logger = get_logger(__name__)

_PLACEHOLDER_ADDRESSES = {"0x...", "", "0x0000000000000000000000000000000000000000"}

# EIP-712 domain — matches RiskRouter.sol constructor: EIP712("RiskRouter", "1")
_DOMAIN_NAME = "RiskRouter"
_DOMAIN_VERSION = "1"

# Struct types for sign_typed_data (EIP712Domain excluded — handled by web3.py)
_TRADE_INTENT_TYPES: dict = {
    "TradeIntent": [
        {"name": "agentId",         "type": "uint256"},
        {"name": "agentWallet",     "type": "address"},
        {"name": "pair",            "type": "string"},
        {"name": "action",          "type": "string"},
        {"name": "amountUsdScaled", "type": "uint256"},
        {"name": "maxSlippageBps",  "type": "uint256"},
        {"name": "nonce",           "type": "uint256"},
        {"name": "deadline",        "type": "uint256"},
    ],
}

_MAX_SLIPPAGE_BPS = 50    # 0.5% default
_INTENT_DEADLINE_SECS = 600  # 10 minutes — must exceed worst-case inclusion time

# Gas bumping: resubmit with higher fees if not confirmed within this window
_GAS_BUMP_INTERVAL_SECS = 10
_GAS_BUMP_MULTIPLIER = 1.20  # +20% each bump
_GAS_BUMP_MAX_ATTEMPTS = 3

_ABI_DIR = Path(__file__).parent / "abis"


class LiveEngine(Engine):
    """Live execution engine.

    Constructs an EIP-712 signed TradeIntent, submits it to the Risk Router
    contract, and waits up to 60 seconds for on-chain confirmation.

    Raises RuntimeError if `risk_router_address` is not configured.
    Raises TimeoutError if the transaction is not mined within 60 seconds.
    """

    _CONFIRMATION_TIMEOUT = 120  # seconds — allows time for gas bumping retries

    def __init__(
        self,
        risk_router_address: str,
        chain_id: int,
        agent_id: int,
        wallet_address: str,
        wallet_private_key: str,
        rpc_url: str,
        tx_lock: asyncio.Lock,
    ) -> None:
        self._router_address = risk_router_address
        self._chain_id = chain_id
        self._agent_id = agent_id
        self._wallet_address = wallet_address
        self._wallet_private_key = wallet_private_key
        self._w3 = AsyncWeb3(AsyncWeb3.AsyncHTTPProvider(rpc_url))
        self._tx_lock = tx_lock
        abi = json.loads((_ABI_DIR / "risk_router.json").read_text())
        self._contract = (
            self._w3.eth.contract(
                address=self._w3.to_checksum_address(risk_router_address),
                abi=abi,
            )
            if risk_router_address not in _PLACEHOLDER_ADDRESSES
            else None
        )

    async def execute_swap(
        self,
        intent: TradeIntent,
        current_price: Decimal,
        amount_usd: Decimal,
    ) -> ExecutionResult:
        """Submit the intent to the Risk Router and wait for confirmation."""
        async with self._tx_lock:
            try:
                return await self._swap(intent, current_price, amount_usd)
            except Exception as exc:
                logger.exception(
                    "LiveEngine execute_swap failed",
                    asset=intent.asset,
                    action=intent.action,
                    amount_usd=str(amount_usd),
                )
                raise MCPError(f"execute_swap failed for {intent.asset}: {exc}") from exc

    async def _swap(
        self,
        intent: TradeIntent,
        current_price: Decimal,
        amount_usd: Decimal,
    ) -> ExecutionResult:
        if self._router_address in _PLACEHOLDER_ADDRESSES or self._contract is None:
            raise RuntimeError(
                "LiveEngine: chain.risk_router_address is not configured. "
                "Set it to the real Risk Router address before executing live trades."
            )

        # Fetch current nonce from chain
        nonce: int = await self._contract.functions.getIntentNonce(self._agent_id).call()

        pair = f"{intent.asset}USDT"
        action = "BUY" if intent.action == "LONG" else "SELL"
        amount_usd_scaled = int(amount_usd * 100)
        deadline = int(time()) + _INTENT_DEADLINE_SECS

        domain = {
            "name": _DOMAIN_NAME,
            "version": _DOMAIN_VERSION,
            "chainId": self._chain_id,
            "verifyingContract": self._router_address,
        }
        message = {
            "agentId":         self._agent_id,
            "agentWallet":     self._wallet_address,
            "pair":            pair,
            "action":          action,
            "amountUsdScaled": amount_usd_scaled,
            "maxSlippageBps":  _MAX_SLIPPAGE_BPS,
            "nonce":           nonce,
            "deadline":        deadline,
        }

        logger.debug(
            "building trade intent",
            asset=intent.asset,
            action=action,
            pair=pair,
            amount_usd=str(amount_usd),
            amount_usd_scaled=amount_usd_scaled,
            agent_id=self._agent_id,
            intent_nonce=nonce,
            deadline=deadline,
            router=self._router_address,
            slippage_bps=_MAX_SLIPPAGE_BPS,
        )

        signed = self._w3.eth.account.sign_typed_data(
            self._wallet_private_key,
            domain,
            _TRADE_INTENT_TYPES,
            message,
        )

        # Compute intent hash for checkpoint correlation (matches TS reference)
        intent_hash = keccak(encode(
            ["uint256", "address", "string", "string", "uint256", "uint256"],
            [self._agent_id, self._wallet_address, pair, action, amount_usd_scaled, nonce],
        ))

        addr = self._w3.to_checksum_address(self._wallet_address)
        intent_tuple = (
            self._agent_id,
            self._wallet_address,
            pair,
            action,
            amount_usd_scaled,
            _MAX_SLIPPAGE_BPS,
            nonce,
            deadline,
        )

        tx_nonce = await self._w3.eth.get_transaction_count(addr, "pending")

        # Build EIP-1559 (Type 2) tx with gas bumping loop
        receipt, tx_hash = await self._submit_with_gas_bumping(
            intent_tuple, signed.signature, addr, tx_nonce,
        )

        approved_events = self._contract.events.TradeApproved().process_receipt(receipt)
        if approved_events:
            logger.info("trade approved on-chain", tx_hash=tx_hash, block=receipt["blockNumber"])
            return ExecutionResult(
                status="executed",
                tx_hash=tx_hash,
                executed_price=current_price,
                slippage_pct=Decimal("0"),
                size_usd=amount_usd,
                intent_hash=intent_hash,
            )

        rejected_events = self._contract.events.TradeRejected().process_receipt(receipt)
        reason = rejected_events[0]["args"]["reason"] if rejected_events else "unknown"
        logger.error("trade rejected on-chain", tx_hash=tx_hash, reason=reason)
        return ExecutionResult(
            status="rejected",
            tx_hash=tx_hash,
            executed_price=Decimal("0"),
            slippage_pct=Decimal("0"),
            size_usd=Decimal("0"),
            intent_hash=intent_hash,
            reason=reason,
        )

    async def _submit_with_gas_bumping(
        self,
        intent_tuple: tuple,
        signature: bytes,
        addr: str,
        tx_nonce: int,
    ) -> tuple:  # (receipt, tx_hash_hex)
        """Submit tx with EIP-1559 fees and bump gas if not confirmed quickly.

        Sends the initial tx, then every _GAS_BUMP_INTERVAL_SECS resubmits with
        the same nonce but +20% higher fees (replace-by-fee). Validators pick the
        highest-fee version from the mempool.
        """
        assert self._contract is not None  # guarded by _swap()

        gas_price = await self._w3.eth.gas_price
        priority_fee = await self._w3.eth.max_priority_fee

        # 2x gas_price gives plenty of headroom; actual cost is always base_fee + priority_fee
        max_fee = gas_price * 2

        tx_hash_hex = ""
        for attempt in range(_GAS_BUMP_MAX_ATTEMPTS):
            tx_params: TxParams = {
                "from":                 addr,
                "nonce":                Nonce(tx_nonce),
                "maxFeePerGas":         Wei(max_fee),
                "maxPriorityFeePerGas": Wei(priority_fee),
                "type":                 2,
            }
            tx = await self._contract.functions.submitTradeIntent(
                intent_tuple, signature,
            ).build_transaction(tx_params)
            signed_tx = self._w3.eth.account.sign_transaction(
                tx, self._wallet_private_key,
            )
            tx_hash_bytes = await self._w3.eth.send_raw_transaction(
                signed_tx.raw_transaction,
            )
            tx_hash_hex = tx_hash_bytes.hex()

            logger.info(
                "trade intent submitted to Risk Router",
                tx_hash=tx_hash_hex,
                router=self._router_address,
                attempt=attempt + 1,
                max_fee_gwei=round(max_fee / 10**9, 4),
                priority_fee_gwei=round(priority_fee / 10**9, 4),
            )

            try:
                receipt = await asyncio.wait_for(
                    self._w3.eth.wait_for_transaction_receipt(
                        tx_hash_bytes, timeout=_GAS_BUMP_INTERVAL_SECS,
                    ),
                    timeout=_GAS_BUMP_INTERVAL_SECS + 5,
                )
                return receipt, tx_hash_hex
            except (asyncio.TimeoutError, TimeoutError, TimeExhausted):
                if attempt == _GAS_BUMP_MAX_ATTEMPTS - 1:
                    raise TimeoutError(
                        f"LiveEngine: tx {tx_hash_hex} not confirmed after "
                        f"{_GAS_BUMP_MAX_ATTEMPTS} attempts "
                        f"({_GAS_BUMP_MAX_ATTEMPTS * _GAS_BUMP_INTERVAL_SECS}s)"
                    )
                # Bump fees for next replacement tx (same nonce)
                max_fee = int(max_fee * _GAS_BUMP_MULTIPLIER)
                priority_fee = int(priority_fee * _GAS_BUMP_MULTIPLIER)
                logger.warning(
                    "tx not confirmed, bumping gas",
                    tx_hash=tx_hash_hex,
                    next_attempt=attempt + 2,
                    new_max_fee_gwei=round(max_fee / 10**9, 4),
                    new_priority_fee_gwei=round(priority_fee / 10**9, 4),
                )

        # Unreachable, but satisfies type checker
        raise TimeoutError("LiveEngine: gas bumping loop exhausted")
