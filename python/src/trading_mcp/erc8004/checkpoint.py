"""EIP-712 signed checkpoints for trade decisions.

Produces a machine-verifiable checkpoint hash for each trade. The checkpoint
is signed by the agent's wallet and the hash is submitted to the
ValidationRegistry so validators can score the agent's decisions on-chain.

Port of src/explainability/checkpoint.ts from the reference implementation.
"""
import time

from eth_account import Account
from eth_account.messages import encode_typed_data
from eth_hash.auto import keccak

# ---------------------------------------------------------------------------
# EIP-712 domain & types (must match the Solidity/TS definitions exactly)
# ---------------------------------------------------------------------------

CHECKPOINT_TYPES = {
    "TradeCheckpoint": [
        {"name": "agentId", "type": "uint256"},
        {"name": "timestamp", "type": "uint256"},
        {"name": "action", "type": "string"},
        {"name": "asset", "type": "string"},
        {"name": "pair", "type": "string"},
        {"name": "amountUsdScaled", "type": "uint256"},
        {"name": "priceUsdScaled", "type": "uint256"},
        {"name": "reasoningHash", "type": "bytes32"},
        {"name": "confidenceScaled", "type": "uint256"},
        {"name": "intentHash", "type": "bytes32"},
    ],
}

def build_domain(registry_address: str, chain_id: int) -> dict:
    return {
        "name": "AITradingAgent",
        "version": "1",
        "chainId": chain_id,
        "verifyingContract": registry_address,
    }


def reasoning_hash(reasoning: str) -> bytes:
    """keccak256 of the UTF-8 reasoning string (32 bytes)."""
    return keccak(reasoning.encode())


def build_checkpoint_value(
    agent_id: int,
    action: str,
    asset: str,
    amount_usd: float,
    price_usd: float,
    reasoning: str,
    intent_hash: bytes,
    confidence: float = 0.8,
) -> dict:
    """Build the EIP-712 value dict from trade parameters.

    Scaled fields match the TS reference:
      amountUsdScaled = round(amountUsd * 100)
      priceUsdScaled  = round(priceUsd * 100)
      confidenceScaled = round(confidence * 1000)
    """
    return {
        "agentId": agent_id,
        "timestamp": int(time.time()),
        "action": action,
        "asset": asset,
        "pair": f"{asset}USD",
        "amountUsdScaled": round(amount_usd * 100),
        "priceUsdScaled": round(price_usd * 100),
        "reasoningHash": reasoning_hash(reasoning),
        "confidenceScaled": round(confidence * 1000),
        "intentHash": intent_hash,
    }


def sign_checkpoint(
    value: dict,
    domain: dict,
    private_key: str,
) -> tuple[str, bytes]:
    """Sign an EIP-712 checkpoint and return (signature_hex, checkpoint_hash).

    checkpoint_hash is the 32-byte EIP-712 struct hash (digest) that gets
    submitted to the ValidationRegistry.
    """
    full_message = {
        "types": {
            "EIP712Domain": [
                {"name": "name", "type": "string"},
                {"name": "version", "type": "string"},
                {"name": "chainId", "type": "uint256"},
                {"name": "verifyingContract", "type": "address"},
            ],
            **CHECKPOINT_TYPES,
        },
        "primaryType": "TradeCheckpoint",
        "domain": domain,
        "message": value,
    }
    signable = encode_typed_data(full_message=full_message)
    signed = Account.sign_message(signable, private_key=private_key)
    return signed.signature.hex(), signable.body
