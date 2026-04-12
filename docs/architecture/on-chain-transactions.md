# On-Chain Transaction Strategy

How the platform submits transactions to Ethereum (Sepolia), handles gas, and recovers from failures.

## Transaction Types

| Contract | Purpose | Critical? |
|----------|---------|-----------|
| RiskRouter | `submitTradeIntent` - EIP-712 signed trade validation | Yes - blocks trade execution |
| AgentRegistry | `register` - mint agent NFT | Yes - one-time setup |
| ValidationRegistry | `postEIP712Attestation` - checkpoint evidence | No - non-fatal if missed |
| ReputationRegistry | `submitFeedback` - scoring | No - non-fatal if missed |

## EIP-1559 (Type 2) Transactions

All transactions use EIP-1559 dynamic fees, not legacy `gasPrice`.

**Fee calculation:**
```
gasTipCap  = SuggestGasTipCap()          (priority fee for validators)
gasFeeCap  = baseFee * 2 + gasTipCap     (max willing to pay per gas unit)
gasLimit   = EstimateGas() * 1.20        (20% buffer over estimate)
```

Why `baseFee * 2`: gives headroom for 6 consecutive full blocks doubling the base fee.
Actual cost is always `baseFee + tipCap` - overpayment is refunded.

## Gas Bumping (Replace-By-Fee)

When a transaction is not confirmed within 10 seconds, we resubmit with the **same nonce** but 20% higher fees. Validators pick the highest-fee version from the mempool.

**Parameters:**
- Bump interval: 10 seconds
- Bump multiplier: 1.20x (both `gasTipCap` and `gasFeeCap`)
- Max attempts: 3
- Total worst-case wait: ~30 seconds

**Flow:**
```
Attempt 1: send tx (nonce=N, fees=base)
  wait 10s for receipt...
  not confirmed ->

Attempt 2: send tx (nonce=N, fees=base*1.2)
  wait 10s for receipt...
  not confirmed ->

Attempt 3: send tx (nonce=N, fees=base*1.44)
  wait 10s for receipt...
  not confirmed -> return error
```

**Receipt polling**: each wait cycle polls ALL previously sent tx hashes, not just the
latest. If an earlier RBF tx (lower fees) gets confirmed before the replacement, we catch
it immediately instead of waiting for the next bump cycle.

**Edge cases during bumping:**
- `nonce too low` - an earlier tx was confirmed. Check receipts of all sent hashes.
- `already known` - the node already has this exact tx in its mempool (e.g. network glitch
  made us think a prior send failed). The tx IS pending - add its hash to the watch list
  and wait for confirmation. If still not confirmed, bump and continue.
- `replacement transaction underpriced` - our bumped fee isn't high enough to replace the
  pending tx. Bump more aggressively. On the last attempt, falls back to waiting for any
  previously sent tx before giving up.

**Zero-tip floor**: if `gasTipCap` is 0 (common on Sepolia), bumping 0 by 1.2x gives 0.
`bumpFee` applies a 1 gwei floor so replacement txs actually have higher fees.

## Implementation

### Go (`golang/internal/chain/client.go`)

Single `SendTx()` method used by all 11 contract call sites. Handles nonce, gas estimation, EIP-1559 fees, signing, gas bumping, and receipt polling.

Key design: callers don't know about gas bumping. They call `SendTx()` and get back `(*Receipt, error)`. All retry logic is internal.

### Python (`python/src/trading_mcp/engine/live.py`)

`_submit_with_gas_bumping()` in `LiveEngine`. Same algorithm. Uses `web3.py` `build_transaction()` for gas estimation (no explicit buffer - web3.py handles it).

Difference: Python uses `gasPrice * 2` for `maxFeePerGas` instead of `baseFee * 2 + tipCap`. Both work, but the Go formula is more precise.

### TypeScript template (`ai-trading-agent-template/`)

No gas bumping. Uses ethers.js defaults for everything. Single attempt, single confirmation.

## Error Propagation

All contract calls propagate errors to callers - no fire-and-forget pattern in Go. If a transaction fails or times out after all bump attempts, the caller gets an error and decides what to do.

Python's `ValidationRegistry` uses fire-and-forget (`asyncio.create_task` with swallowed exceptions) because checkpoint posts are non-critical. Go does not do this.

## Nonce Management

Both Go and Python use `PendingNonceAt` / `get_transaction_count(pending)` per call. No nonce cache or sequencer.

This is safe because all callers are sequential:
- Go: serialized by Swiftward policy eval (one trade at a time)
- Python: serialized by `asyncio.Lock` (`tx_lock`)

If concurrent sends from the same key are ever needed, a nonce manager would be required.
