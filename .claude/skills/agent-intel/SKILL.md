---
name: agent-intel
description: "Deep on-chain intelligence analysis of hackathon trading agents - validation attestations, reputation feedback, trade intents, sybil detection, and scoring breakdown."
---

# Agent Intel - On-Chain Agent Analysis

Analyze any agent registered on the hackathon's ERC-8004 contracts. Produces a full intelligence report covering registration, trading, validation, reputation, and gaming detection.

## Prerequisites

- `.env` file with `CHAIN_RPC_URL` and `HACKATHON_*` addresses
- `cast` (foundry) installed
- Go toolchain (for leaderboard-spy)

## Workflow

### Step 1: Identify the target

If user provides a **name**, first run the agent list to find the ID:

```bash
HACKATHON_VALIDATION_ADDR=0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1 \
  bash -c 'set -a && . ./.env && set +a && \
  export HACKATHON_VALIDATION_ADDR=0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1 && \
  cd golang && go run ./cmd/leaderboard-spy agents'
```

Find the agent by name in the output. Note its **ID** (first column).

If user provides an **ID** or **wallet address**, use that directly.

### Step 2: Collect on-chain data

Run these queries in parallel using the RPC from `.env` (`CHAIN_RPC_URL`). All are read-only (no gas).

**Contract addresses (hackathon):**
- AgentRegistry: `0x97b07dDc405B0c28B17559aFFE63BdB3632d0ca3`
- ValidationRegistry: `0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1`
- ReputationRegistry: `0x423a9904e39537a9997fbaF0f220d79D7d545763`
- RiskRouter: `0xd6A6952545FF6E6E6681c2d15C59f9EB8F40FdBC`
- HackathonVault: `0x0E7CD8ef9743FEcf94f9103033a044caBD45fC90`

**Agent ID as hex topic** (pad to 32 bytes): e.g., agent 18 = `0x0000000000000000000000000000000000000000000000000000000000000012`

#### 2a. Basic info

```bash
# Wallet nonce and balance
cast nonce <wallet> --rpc-url $RPC
cast balance <wallet> --ether --rpc-url $RPC
```

#### 2b. Attestation notes (via leaderboard-spy)

```bash
HACKATHON_VALIDATION_ADDR=0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1 \
  bash -c 'set -a && . ./.env && set +a && \
  export HACKATHON_VALIDATION_ADDR=0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1 && \
  cd golang && go run ./cmd/leaderboard-spy attestations <AGENT_ID>'
```

#### 2c. Validation events (from ValidationRegistry)

Event signature: `0x5c19b748ded05e13d6fb5776172b6f813692e237572a5c725a43077cb82a67db`

```bash
cast logs --from-block <10580000> --to-block <LATEST> \
  --address 0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1 \
  "0x5c19b748ded05e13d6fb5776172b6f813692e237572a5c725a43077cb82a67db" \
  "<AGENT_ID_HEX_PADDED>" \
  --rpc-url $RPC > /tmp/agent_val.txt
```

Analyze: count events, extract unique validator addresses from topic[2], check if all from same address (self-attestation).

#### 2d. Reputation events (from ReputationRegistry)

Event signature: `0x2957247c48ca9e733e8c76cff7e64dafa0f9402a35dbe2cb1cfc7259077435e4`

```bash
cast logs --from-block <10580000> --to-block <LATEST> \
  --address 0x423a9904e39537a9997fbaF0f220d79D7d545763 \
  "0x2957247c48ca9e733e8c76cff7e64dafa0f9402a35dbe2cb1cfc7259077435e4" \
  "<AGENT_ID_HEX_PADDED>" \
  --rpc-url $RPC > /tmp/agent_rep.txt
```

Analyze with Python:

```python
import re
from collections import Counter

with open('/tmp/agent_rep.txt') as f:
    content = f.read()

events = content.split('- address:')
events = [e for e in events if e.strip()]

validators = {}
scores = []
for event in events:
    topics = re.findall(r'0x[0-9a-fA-F]{64}', event.split('topics:')[1].split(']')[0]) if 'topics:' in event else []
    if len(topics) >= 3:
        validator = '0x' + topics[2][26:]
        validators[validator] = validators.get(validator, 0) + 1
    data_match = re.search(r'data: (0x[0-9a-fA-F]+)', event)
    if data_match:
        data = data_match.group(1)[2:]
        score = int(data[:64], 16)
        scores.append(score)

print(f"Total: {len(events)}, Unique validators: {len(validators)}")
print(f"Score distribution: {dict(Counter(scores).most_common())}")
# Sybil indicator: unique_validators == total_events AND all nonce=1
```

#### 2e. Trade intents (from RiskRouter)

Event signature: `0x536c9b7dd53ffa0a0b01880535f363a405c6a20ebedc6802702927c602852b9b`

```bash
cast logs --from-block <10580000> --to-block <LATEST> \
  --address 0xd6A6952545FF6E6E6681c2d15C59f9EB8F40FdBC \
  "0x536c9b7dd53ffa0a0b01880535f363a405c6a20ebedc6802702927c602852b9b" \
  "<AGENT_ID_HEX_PADDED>" \
  --rpc-url $RPC > /tmp/agent_trades.txt
```

Decode trade data with Python (ABI-encoded: pair string, action string, amountUsdScaled uint256):

```python
for event in events:
    data = re.search(r'data: (0x[0-9a-fA-F]+)', event).group(1)[2:]
    amount_scaled = int(data[128:192], 16)
    amount_usd = amount_scaled / 100.0
    pair_offset = int(data[0:64], 16) * 2
    pair_len = int(data[pair_offset:pair_offset+64], 16)
    pair = bytes.fromhex(data[pair_offset+64:pair_offset+64+pair_len*2]).decode()
    action_offset = int(data[64:128], 16) * 2
    action_len = int(data[action_offset:action_offset+64], 16)
    action = bytes.fromhex(data[action_offset+64:action_offset+64+action_len*2]).decode()
```

### Step 3: Analyze and detect gaming

Check these indicators:

| Signal | What to check |
|--------|---------------|
| **Self-attestation** | All validation attestations posted by the agent's own wallet (or a single address). Check if unique validator count == 1 for the agent's attestations. The validator address in the event is whoever called `postEIP712Attestation` - if it's always the same address, the agent is self-attesting. |
| **Attestation spam** | >100 attestations, notes containing "boost" |
| **Reputation sybil** | Many feedbacks, each from unique address with nonce=1 |
| **Wash trading** | Many small trades ($1-$10), repetitive pairs |
| **Score inflation** | Scores consistently 95-100 across all feedbacks |

### Step 4: Produce the report

Structure the output as:

```
## [Agent Name] (Agent #ID) - Intelligence Report

### Registration
- Wallet, operator, description, capabilities
- Wallet nonce and balance (activity indicator)
- Vault status

### Leaderboard Scores
- Reputation: X, Validation: X, Trades: X

### Trading Activity
- Total intents, pairs traded, actions, volume
- Average trade size, time range

### Validation Attestations
- Count, unique validators, score distribution
- Self-attestation detected? Boost entries?

### Reputation Feedback
- Count, unique validators, score distribution
- Sybil indicators (unique addresses, nonce=1 pattern)

### Gaming Assessment
- Summary of detected gaming techniques
- Comparison to baseline (our agents)
```

## Scoring Formula Reference

The leaderboard reputation score is calculated by a judge bot (runs every ~4h):

- Validation avg x 0.50 = 0-50 pts
- Trade count x 3 (capped at 10 trades = 30 pts)
- Vault claimed = 10 pts
- Any attestation posted = 10 pts
- **Max: 100**

The `getAverageScore` on ReputationRegistry returns the average of all `giveFeedback` values posted by external wallets. This is separate from the judge bot score but displayed as "Reputation" on the leaderboard.

## Our Agents for Comparison

| Agent | ID | Reputation | Validation | Attestations | Trades |
|-------|-----|-----------|-----------|-------------|--------|
| Swiftward Alpha | 32 | 70 | 100 | 5 | 2 |
| SWIFTWARD_BETA | 42 | 0 | 0 | 0 | 0 |
| Swiftward Gamma | 43 | 31 | 0 | 0 | 1 |
| Random Trader | 37 | 77 | 100 | 613 | 5 |
