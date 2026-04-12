# Blockchain Setup Guide

On-chain registration and trading for the ERC-8004 hackathon.

## Background

There are two sets of smart contracts on Sepolia testnet:

**Official ERC-8004** - the real standard. We registered here first (agentId=1612). Good for the "Best Trustless Agent" prize category.

**Hackathon organizer's** - deployed by the template repo. The leaderboard, RiskRouter, and judge bot all read from these. We MUST register here to appear on the leaderboard.

Both registrations use the same wallet and IPFS metadata. An agent registers on both to show standard compliance AND appear on the hackathon leaderboard.

### Contract Addresses

| Contract | Official ERC-8004 | Hackathon |
|---|---|---|
| AgentRegistry | `0x8004A818BFB912233c491871b3d84c89A494BD9e` | `0x97b07dDc405B0c28B17559aFFE63BdB3632d0ca3` |
| ValidationRegistry | `0x8004Cb1BF31DAf7788923b405b754f57acEB4272` | `0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1` |
| ReputationRegistry | `0x8004B663056A597Dffe9eCcC1965A193B7388713` | `0x423a9904e39537a9997fbaF0f220d79D7d545763` |
| RiskRouter | - | `0xd6A6952545FF6E6E6681c2d15C59f9EB8F40FdBC` |
| HackathonVault | - | `0x0E7CD8ef9743FEcf94f9103033a044caBD45fC90` |

## Part 1: One-Time Platform Setup

### 1.1 Sepolia RPC

Sign up at https://developer.metamask.io (Infura). Create API key, enable Sepolia.

```
CHAIN_RPC_URL=https://sepolia.infura.io/v3/YOUR_KEY
CHAIN_ID=11155111
```

### 1.2 Contract addresses in .env

Two sets of env vars - one per registry:

```
# Standard ERC-8004 contracts (for `make register-agent-standard`)
ERC8004_IDENTITY_ADDR=0x8004A818BFB912233c491871b3d84c89A494BD9e
ERC8004_VALIDATION_ADDR=0x8004Cb1BF31DAf7788923b405b754f57acEB4272
ERC8004_REPUTATION_ADDR=0x8004B663056A597Dffe9eCcC1965A193B7388713

# Hackathon contracts (for `make register-agent-hackathon`, and runtime)
HACKATHON_IDENTITY_ADDR=0x97b07dDc405B0c28B17559aFFE63BdB3632d0ca3
HACKATHON_VALIDATION_ADDR=0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1
HACKATHON_REPUTATION_ADDR=0x423a9904e39537a9997fbaF0f220d79D7d545763
RISK_ROUTER_ADDR=0xd6A6952545FF6E6E6681c2d15C59f9EB8F40FdBC
```

The trading server at runtime uses `HACKATHON_*` addresses (configured in `.env` and `compose.yaml`).

### 1.3 Validator wallet

```bash
make gen-wallets
```

```
CHAIN_VALIDATOR_PRIVATE_KEY=0x...
CHAIN_VALIDATOR_ADDR=0x...
```

### 1.4 IPFS (Pinata)

Sign up at https://app.pinata.cloud, create API key:

```
PINATA_JWT=eyJ...
```

## Part 2: Per-Agent Setup

Replace `X` with agent name (e.g. `DELTA_GO`).

### 2.1 Generate keypair

```bash
make gen-wallets
```

```
AGENT_X_PRIVATE_KEY=0x...
```

### 2.2 Fund the wallet

Get address from the private key:
```bash
cast wallet address 0x...YOUR_PRIVATE_KEY
```

Get ~0.05 test ETH from https://cloud.google.com/application/web3/faucet/ethereum/sepolia

### 2.3 Create and upload agent metadata to IPFS

Create `erc8004/agents/x.json`:

```json
{
  "type": "https://eips.ethereum.org/EIPS/eip-8004#registration-v1",
  "name": "Swiftward Alpha",
  "description": "Policy-enforced AI trading agent with deterministic risk management by Swiftward.",
  "image": "ipfs://QmPCtfkHorHpo5Cxyjss9THtyMLeMBeaJCmhmZCi58LdWj",
  "services": [
    {
      "name": "evidence",
      "endpoint": "https://api.swiftward.dev/v1/evidence",
      "version": "1.0.0"
    }
  ],
  "active": true,
  "supportedTrust": ["validation", "reputation"]
}
```

```bash
make erc8004-ipfs-upload AGENT=X
```

```
AGENT_X_REGISTRATION_URI=ipfs://QmXXX...
```

### 2.4 Register on hackathon's AgentRegistry

```bash
make register-agent-hackathon AGENT=X
```

Uses `HACKATHON_IDENTITY_ADDR`. Prints the new agentId - store it in `.env`:

```
AGENT_X_ERC8004_AGENT_ID=33
```

### 2.5 Register on standard ERC-8004 registry

```bash
make register-agent-standard AGENT=X
```

Uses `ERC8004_IDENTITY_ADDR`. Gives a separate agentId on the standard registry. Not needed for the leaderboard, but good for the ERC-8004 compliance story.

### 2.6 Claim vault capital (optional)

The HackathonVault allocates 0.001 ETH per team. Claiming gives +10 reputation points from the judge bot. RiskRouter does NOT check vault balance.

### 2.7 Smoke test

```bash
make erc8004-validate AGENT=X
```

## Part 3: How Trading Works

After setup, every trade goes through this flow automatically:

```
Agent decides: "BUY $100 of ETH-USD"
    |
    v
1. Swiftward policy engine (gRPC, internal)
    Checks: drawdown, position size, concentration, loss streaks, $1000 cap
    Result: APPROVE or REJECT
    |  approved
    v
2. RiskRouter on Sepolia (blockchain transaction, costs gas)
    Checks: position < $1K default cap, trades per hour
    Result: TradeApproved or TradeRejected event on-chain
    |  approved
    v
3. Kraken exchange (paper or live)
    Execute the trade, get fill price/qty/fee
    |  filled
    v
4. Evidence recording (parallel)
    a. Postgres hash chain - tamper-evident local trail
    b. ValidationRegistry on Sepolia - EIP-712 signed checkpoint on-chain
```

Swiftward rules are strictly tighter than RiskRouter's, so RiskRouter should never reject what Swiftward approved.

## Part 4: Leaderboard

URL: `leaderboard.stevekimoi.me`

| What it shows | Where it reads from | Contract function |
|---|---|---|
| Agent name | AgentRegistry | `getAgent(agentId)` |
| Trade count | RiskRouter | `getTradeRecord(agentId)` |
| Validation score | ValidationRegistry | `getAverageValidationScore(agentId)` |
| Reputation | ReputationRegistry | `getAverageScore(agentId)` |
| Vault claimed | HackathonVault | `hasClaimed(agentId)` |

**Ranking**: validation score primary, reputation as tiebreaker.

**Judge bot** (`auto-reputation.ts`) runs every 4h on organizer's server:
- Validation avg score x 0.50 = 0-50 pts
- Trade count x 3 (capped at 10 trades) = 0-30 pts
- Vault claimed = 0 or 10 pts
- Any attestation posted = 0 or 10 pts

Can only rate each agent once. Already rated agents #1-24 as of Apr 7.

## Checklist

| # | Step | Command | .env var | Done? |
|---|------|---------|----------|-------|
| 1 | Sepolia RPC | - | `CHAIN_RPC_URL`, `CHAIN_ID` | Yes |
| 2 | Contract addresses | - | `ERC8004_*`, `HACKATHON_*`, `RISK_ROUTER_ADDR` | Yes |
| 3 | Validator wallet | `make gen-wallets` | `CHAIN_VALIDATOR_*` | Yes |
| 4 | IPFS (Pinata) | - | `PINATA_JWT` | Yes |
| 5 | Agent keypair | `make gen-wallets` | `AGENT_X_PRIVATE_KEY` | Yes |
| 6 | Fund wallet | Sepolia faucet | - | Yes |
| 7 | Upload metadata | `make erc8004-ipfs-upload` | `AGENT_X_REGISTRATION_URI` | Yes |
| 8 | Register (hackathon) | `make register-agent-hackathon` | `AGENT_X_ERC8004_AGENT_ID` | Yes (agentId=32) |
| 9 | Register (standard) | `make register-agent-standard` | - | Yes (agentId=2761) |
| 10 | Claim vault | TBD | - | **TODO** |
| 11 | Start trading | `make demo` | - | - |
