# AI Trading Agents Hackathon - Q&A from Discord

Compiled from #general-qa-stage, #participants-chat-ai-trading-agents, Surge AMA, and support tickets (March 30 - April 11, 2026). Only confirmed facts from organizers (Steve, Zofia, Nathan Kay, Kraken mentors) and directly verifiable sources (contract code on Etherscan, live leaderboard).

**Last updated:** April 11, 2026  
**Deadline:** Sunday, April 12, 2026, 18:00 CET / 11:00 AM EST

Legend: ✅ official answer | ⚠️ partially answered | ❓ open

---

## General

### ✅ Q: Can I participate in both ERC-8004 and Kraken challenges?

**Yes.** You can do both.

*Source: Q&A session (March 30)*

### ✅ Q: Can we use a simulated execution environment instead of real integrations?

- **Kraken Challenge**: No. Requires real Kraken CLI execution with real funds. Rankings are based on net PnL verified via a read-only API key.
- **ERC-8004 Challenge**: Test funds are OK. Requires on-chain activity through the Hackathon Vault and Risk Router, but you don't need real capital.

*Source: Steve (March 31), Kraken Mentors via Zofia (April 1)*

### ❓ Q: Can I use my own data source (e.g. broker account) for price data?

**Unanswered.** Asked during Q&A session, no official response.

---

## Kraken Challenge

### ✅ Q: Is real money required for the Kraken Challenge leaderboard?

**YES.** Paper trading in kraken-cli is a local simulation - balances, orders, and fills live in a JSON file on your machine. There is no server-side record that Kraken or lablab can see or audit.

To be ranked on the leaderboard, participants must:
- Trade with real funds on Kraken
- Submit a read-only API key linked to their trading account for verification

Paper mode is for building and testing agents before going live. The read-only key only grants view access to trade history - no execution or withdrawal permissions.

*Source: Kraken Mentors via Zofia (April 1)*

### ❓ Q: How is starting capital fairness handled? Someone with $10,000 has an advantage over someone with $10.

**Open question.** Since real money is required, teams with more capital have an inherent advantage. The hackathon page says ranking is by "net PnL" without specifying whether that's absolute or percentage-based. Kraken mentors said: "This needs a clear ruling from lablab/Surge: absolute PnL vs. percentage return, minimum capital requirements, etc. We provide the trading tool; the ranking methodology is on the hackathon organizers to define."

Multiple participants echoed this concern (April 2): using absolute PnL with unequal capital creates uneven conditions. Suggested alternatives: percentage return or win rate.

*Source: Kraken Mentors via Zofia (April 1), Atikin_NT (April 2)*

### ✅ Q: Is Kraken CLI available on Windows?

No native Windows support. Use WSL (Windows Subsystem for Linux) - confirmed working by participants.

*Source: Predator47 (April 1)*

### ✅ Q: Where is the leaderboard? How does an agent appear on it? What's the scoring formula?

**ERC-8004 leaderboard is live** on the event page: https://lablab.ai/ai-hackathons/ai-trading-agents/live - updates every 30 seconds.

**Official scoring formula (Steve, April 10)** - appears to compute a 100-pt "validation-side" score:
- Validation avg score × 0.5 → 0-50 pts
- Approved trades × 3, capped at 10 trades → 0-30 pts
- Vault capital claimed → 10 pts
- Activity bonus (at least one checkpoint posted) → 10 pts

Steve added: "Leaderboard ranking = combined score (validation 50% + reputation 50%)". The cleanest reading: the 4 bullets above produce a validation-side score (0-100), and the final leaderboard rank is 50% of that + 50% of the reputation score. The live leaderboard columns (Validation, Reputation) are consistent with this.

**Judge bot runs every 4 hours** automatically. Validation and reputation update each cycle. No manual rescore needed.

**Critical**: On-chain scores are **inputs** to final judging, not the ranking itself. Judges also review trading strategy, code quality, presentation, and business value. Per Steve: "Inflating on-chain scores won't move your final placement." Corollary: there is no reason to post a validation score below 100 - it only hurts your leaderboard position and provides no signal judges weigh separately.

*Source: Steve (April 10, announcement)*

### ✅ Q: How do I submit my Kraken API key?

**Updated (April 4).** The Kraken Submission Form on the lablab event page now accepts:
- Read-only Kraken API key
- Account ID / username

Teams that already submitted were notified to update their submission.

*Source: Steve (April 4, announcement)*

### ✅ Q: Are there corrections to the Kraken CLI docs?

**Yes (April 3 announcement).** Major fixes to the tutorial docs:
- **No `--sandbox` flag** - use `kraken paper` subcommands instead (`kraken paper init --balance 10000 --currency USD`, `kraken paper buy BTCUSD 0.001`, etc.)
- **Output format**: use `-o json` not `--json` (`kraken -o json ticker BTCUSD`)
- **Ticker**: pair is positional - `kraken ticker BTCUSD` not `ticker --pair BTCUSD`
- **Orders**: use `order buy` / `order sell` not `order add` (`kraken order buy BTCUSD 0.001 --type market`)
- **MCP**: runs over stdio not HTTP - use `kraken mcp` not `kraken mcp serve --port 8080`
- **Ticker symbol**: use `BTCUSD` not `XBTUSD` throughout
- **Rate limits**: automatic retry with exponential backoff for transient/5xx errors; rate-limit errors surfaced immediately with actionable fields
- Remove `KRAKEN_SANDBOX` from `.env`

*Source: Steve (April 3, announcement - "Kraken CLI Docs Fix")*

### ❓ Q: How is social engagement measured for the Kraken challenge?

**Unanswered.** No official response yet.

### ✅ Q: Kraken is not available in my country (India, Pakistan, etc.). What should I do?

Kraken isn't currently available in India, Pakistan, and some other regions due to licensing restrictions. You can still use paper trading to build and test your agent, but paper trading won't count for the Kraken leaderboard.

Workaround shared by a participant: build native Kraken REST API calls directly instead of using the CLI binary.

*Source: Steve (April 2), participant workaround (April 2)*

---

## ERC-8004 Challenge

### ✅ Q: What are the minimum requirements for the ERC-8004 challenge?

You don't have to implement everything from ERC-8004, but just doing a small part probably won't be enough. Your agent should:
- Register identity (ERC-721 agent identity)
- Build some kind of reputation from results
- Produce validation artifacts (trade intents, risk checks, etc.)

It's about showing a **complete trustless flow**, not just one piece. Keep it simple, but it should feel like a working system, not just a partial integration.

*Source: Zofia (March 31)*

### ✅ Q: Should teams use hackathon-provided registries or deploy their own?

**Use shared contracts.** Using shared contracts is the only way judges will compare results. The leaderboard reads from these shared addresses only. Teams who already self-deployed should re-register on the shared AgentRegistry.

*Source: Steve (April 1, private ticket; April 3, announcement)*

### ✅ Q: Does the Risk Router actually execute trades on a DEX?

**No.** The hackathon page marketing copy mentioned "DEX execution via a whitelisted Risk Router contract (Uniswap-style routers)," but the deployed shared `RiskRouter` only validates signed `TradeIntent` payloads, enforces risk limits, and emits events. It does not route to any DEX, does not swap tokens, and does not spend from the Vault.

**The real flow** (confirmed by Steve, April 3 and April 10 scoring formula):
1. Register agent on `AgentRegistry`
2. (Optional, +10 pts) `claimAllocation` on `HackathonVault`
3. Submit signed `TradeIntent` to `RiskRouter` (validated, counted as "approved trade")
4. Post checkpoint to `ValidationRegistry` (counts toward validation score + activity bonus)

Judging is based on these on-chain events, not actual token swaps. If you want real execution, that's the Kraken challenge.

*Source: `RiskRouter.sol` contract code, Steve (April 3, April 10)*

### ❓ Q: Does implementing just the Validation Registry + a strategy qualify for the ERC-8004 challenge?

**Unanswered** as of April 2.

*Source: Kai (April 2, #general)*

### ✅ Q: How do agents set their own risk params on the shared RiskRouter?

`setRiskParams()` is `onlyOwner` - teams cannot call it themselves. **Steve (the contract owner) updates risk params on request.** Post your Agent ID in Discord and ask for new limits.

Example (Agent #64, Sims, April 11): Steve set `maxPositionUsd: 100`, `maxDrawdownBps: 1000` (10%), `maxTradesPerHour: 1000`.

Default fallback (if owner has not configured for your agent): $1,000 cap per trade, no hourly trade limit, only position size is checked (`RiskRouter.sol:233-235`).

*Source: `RiskRouter.sol` contract source, Steve (April 11, confirmed in-chat)*

### ✅ Q: `postEIP712Attestation` reverts with "not an authorized validator" even when whitelisted. Why?

**Contract bug - now fixed (Steve, April 10).** The original `postEIP712Attestation` used an internal `this.postAttestation(...)` call, which changed `msg.sender` to the ValidationRegistry contract itself (not in the whitelist), causing the `onlyValidator` modifier to revert.

**Fix**: Contract-level patch deployed April 10. Whitelisted operators can retry without code changes.

**Workaround (if still blocked)**: Call `postAttestation` directly from your whitelisted operator wallet instead:
```python
validation.functions.postAttestation(
    agent_id,
    checkpoint_hash,
    score,          # 0-100
    1,              # ProofType.EIP712
    b"",            # empty proof bytes
    "notes"
).build_transaction({...})
```

*Source: Steve (April 10), lemonSn participant writeup (April 10)*

### ✅ Q: How do newly registered agents (after validator whitelist closed) get validation scores?

Steve adds post-cutoff agents to the whitelist in subsequent judge bot runs. If your validation score is still 0 after a full cycle (4 hours) and your agent has posted checkpoints, drop your Agent ID and wallet in Discord and tag Steve.

*Source: Steve (April 10-11, confirmed multiple whitelist additions in-chat)*

---

## Sandbox Capital & Hackathon Vault

### ✅ Q: How do we claim funds from the Hackathon Capital Vault? What are the contract addresses?

Contract addresses are published and verified on Sepolia Testnet (Chain ID: 11155111).

| Contract | Address |
|----------|---------|
| AgentRegistry | `0x97b07dDc405B0c28B17559aFFE63BdB3632d0ca3` |
| HackathonVault | `0x0E7CD8ef9743FEcf94f9103033a044caBD45fC90` |
| RiskRouter | `0xd6A6952545FF6E6E6681c2d15C59f9EB8F40FdBC` |
| ReputationRegistry | `0x423a9904e39537a9997fbaF0f220d79D7d545763` |
| ValidationRegistry | `0x92bF63E5C7Ac6980f237a7164Ab413BE226187F1` |

All contracts are verified on Etherscan. Full docs: [SHARED_CONTRACTS.md](https://github.com/Stephen-Kimoi/ai-trading-agent-template/blob/main/SHARED_CONTRACTS.md)

**How to claim:**
1. Register your agent on AgentRegistry (returns an `agentId`, ERC-721)
2. Call `claimAllocation(agentId)` on HackathonVault
3. One claim per agentId, enforced on-chain

**Vault now works (confirmed April 6).** Steve made changes to the vault contract. Vlad Filip confirmed claiming works. Note: the public SHARED_CONTRACTS.md still says 0.05 ETH per team, but the organizer's admin scripts show allocation was lowered to 0.001 ETH on April 6. The actual amount you receive may differ from the docs.

**Vault claim is optional for submission, but worth 10 points in the leaderboard formula.** Steve (April 6): "The claimAllocation step is not required to build and submit. You can register on AgentRegistry, submit trade intents via RiskRouter, and post checkpoints to ValidationRegistry without ever touching the vault." However, the April 10 scoring formula explicitly grants **10 pts for "Vault capital claimed"** - so skipping it costs you leaderboard position even though it does not block submission.

**Integration details (from the template):**
- Two wallets: `operatorWallet` owns the ERC-721 and pays gas; `agentWallet` signs trade intents
- `TradeIntent` uses EIP-712 typed data signing
- Use `simulateIntent()` to dry-run before submitting
- Post checkpoints to `ValidationRegistry` after each decision (EIP-712 digest of reasoning)
- Steve (April 11) confirmed the default RiskRouter cap is **10 trades/hour**. For custom params, post your Agent ID in Discord and ask the owner to call `setRiskParams()` (see ERC-8004 → RiskRouter Q&A)

**Judging reads from these contracts only.** See the leaderboard Q&A above for the official scoring formula (Steve, April 10).

**To get Sepolia ETH for gas**, use one of these faucets:
- https://cloud.google.com/application/web3/faucet/ethereum/sepolia
- https://docs.metamask.io/developer-tools/faucet
- https://www.infura.io/faucet/sepolia

*Source: Steve (March 31 initial, April 3 announcement, April 6 vault fix + optional clarification), Vlad Filip (April 6, confirmed working)*

### ✅ Q: Is the Hackathon Vault the same as Kraken paper trading capital?

**No, completely separate:**
- **Kraken paper trading**: local JSON-file simulation on your machine, imaginary capital, configurable starting balance. Does not count for the Kraken leaderboard (requires real funds).
- **Hackathon Vault**: on-chain Sepolia allocation (~0.001 ETH per agent on the current leaderboard; docs say 0.05 ETH but the actual amount is lower). Used only for the ERC-8004 challenge, optional for submission, worth 10 pts in scoring.

*Source: Zofia / Kraken Mentors (April 1), Steve (April 3, April 6)*

---

## Surge Platform

### ⚠️ Q: What do we need to register a project on Surge?

Based on the Surge AMA (April 1), project registration on early.surge.xyz appears to require a video, pitch deck, and website. You don't need to do this immediately - you can register before the end of the hackathon.

Note: it's unclear how "build in public" special prize works if project registration requires finished materials.

*Source: Surge AMA (April 1)*

### ⚠️ Q: What is the Surge 30-Day Challenge?

A streaming/building-in-public program with a rewards pool. Details at [challenge.surge.xyz](https://challenge.surge.xyz/). Discussed during the Surge AMA but specifics on how it ties to the hackathon are unclear.

*Source: Nathan Kay, Surge AMA (April 1)*

### ❓ Q: Do all team members need a Surge account?

**Unanswered** as of April 1.

---

## Logistics

### ❓ Q: Will the Prism API calls stop once the hackathon expires?

**Unanswered** as of April 3.

*Source: Teacup (April 3)*

### ✅ Q: When are mentors available?

Mentors operate in **CET timezone** (no specific hours). Not 24/7 support. Tag @Mentors in the Discord channel and wait for a response.

*Source: Zofia (March 31)*

### ✅ Q: Where do I submit my project? Lablab or Surge?

**Submit on lablab first, then Surge** (Steve, April 11). After lablab submission, the project auto-creates as a draft on Surge. You'll receive an email to log into Surge so your project also appears there.

**Mandatory for prize eligibility**: You MUST log into Surge within 24 hours after the deadline (Zofia, April 10).

**Deadline**: Sunday, April 12, 2026, 18:00 CET / 11:00 AM EST.

**ERC-8004 submission form (updated April 11)**: Include your agent address and agent name exactly as they appear on the leaderboard. Teams who already submitted should go back and fill this in.

*Source: Zofia (April 10), Steve (April 11)*

### ✅ Q: Do we need a full cloud deployment, or does a landing page suffice?

**Full deployment not required.** A landing page clearly explaining how your agent works is a plus and can be submitted as the application URL. Demos can be recorded from localhost.

*Source: Steve (April 11)*

### ✅ Q: Is there a live Q&A session?

**Yes.** Thursday, April 9, 2026, 6:00-6:30 PM CEST on Discord (General Q&A Stage).

*Source: Zofia (April 7, announcement)*

### ❓ Q: Can we test/run multiple different agents?

**Unanswered** as of April 7.

*Source: Konstantin Trunin (April 7)*

### ⚠️ Q: How does leaderboard trade count feed into scoring?

**For scoring**: only the first 10 approved trades count (3 pts each → max 30 pts). Posting more than 10 does not improve your score on this component.

**Raw trade count display**: the `Intents` column on the leaderboard shows lifetime on-chain intents. Participants observed non-monotonic behavior (counts going up then down); organizers have not explained this. It does not affect scoring.

*Source: Steve (April 10, scoring formula)*

### ✅ Q: Is the Kraken CLI required for the ERC-8004 track?

**No.** The two challenges are independent per the official hackathon page. Kraken CLI is only needed for the Kraken challenge (real-funds trading ranked by net PnL). For ERC-8004, you interact with the Sepolia contracts directly.

*Source: Official hackathon page - "two equal challenges"*

### ✅ Q: How do I get mentor feedback on my concept?

Create a ticket in the #create-a-ticket channel with a clear description of your question. Tag @Mentors in the general chat. Note: mentor validation is **not mandatory** - it's for additional help.

**WARNING: Do NOT click on external "support" links in Discord. Scammers are active.** Only use the official #create-a-ticket channel.

*Source: Zofia, Dave McFly (March 31)*

---

## Summary

### Updated since last update (April 7 → April 11)

- **Scoring formula published** (Steve, April 10): validation×0.5 (50pts) + approved trades×3 capped at 10 (30pts) + vault claim (10pts) + activity bonus (10pts). Leaderboard ranking = 50% validation + 50% reputation.
- **Judge bot cadence**: runs every 4 hours automatically.
- **On-chain scores are inputs, not final ranking** - judges review full submission (strategy, code, presentation, business value). Gaming on-chain won't move placement.
- **`postEIP712Attestation` bug fixed** (April 10). Was reverting due to internal `this.` call changing msg.sender. Workaround: call `postAttestation` directly.
- **RiskRouter params**: Steve updates `setRiskParams` on request (owner-only). Post Agent ID in Discord to get custom limits.
- **Late-registered agents**: Steve manually whitelists post-cutoff agents in subsequent judge bot runs.
- **Submission flow confirmed**: Lablab first → auto-creates Surge draft → MUST log into Surge within 24h of deadline (mandatory for prize eligibility).
- **ERC-8004 submission form** updated (April 11) to require agent address + agent name as shown on leaderboard.
- **Deployment**: full cloud deployment not required; landing page is sufficient and recommended.
- **Deadline**: Sunday, April 12, 2026, 18:00 CET / 11:00 AM EST.

### Open Questions

| # | Question | Status |
|---|----------|--------|
| 1 | Does implementing only ValidationRegistry + a strategy qualify for the ERC-8004 challenge? | Unanswered |
| 2 | Kraken challenge ranking: absolute PnL vs % return? Minimum capital requirement? | Waiting on lablab/Surge |
| 3 | Do all team members need individual Surge accounts? | Unanswered |
| 4 | How does the Kraken "build in public" (Social Engagement) score get measured? | Unanswered |
| 5 | Can agents fetch price data from their own broker/data source? | Unanswered |
| 6 | Will Prism API credits keep working after hackathon ends? | Unanswered |
| 7 | Can one team register and run multiple distinct agents? | Unanswered |
| 8 | Why does the leaderboard `Intents` count move non-monotonically? | Unanswered (does not affect scoring) |
