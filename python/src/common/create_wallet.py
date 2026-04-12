"""Generate a new EVM wallet (private key + address).

Usage:
    uv run python -m common.create_wallet
"""
from eth_account import Account

if __name__ == "__main__":
    acct = Account.create()
    print(f"Address:     {acct.address}")
    print(f"Private key: {acct.key.hex()}")
    print()
    print("Add to python/config/config.yaml:")
    print("  erc8004:")
    print(f"    agent_wallet_private_key: \"{acct.key.hex()}\"")
