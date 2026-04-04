# AI Trading Agents Hackathon - Q&A from Discord

Compiled from Discord channels: #general-qa-stage, #participants-chat-ai-trading-agents, Surge AMA, and private support tickets (March 30 - April 2, 2026).
Questions from participants with official answers from lablab.ai team and Kraken mentors.

**Last updated: April 2, 2026**

Legend: ✅ official answer available | ❓ waiting on organizers | ⚠️ partially answered

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

### ⚠️ Q: Where is the leaderboard? How does an agent appear on it?

The Kraken team hasn't communicated details yet, but full information will be provided during the hackathon. This does not affect your hackathon progress - keep building.

*Source: Steve (April 1, private ticket)*

### ✅ Q: How is social engagement measured for the Kraken challenge?

its internal

*Source: Zofia at General Q&A Stage (March 30)*

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

**Use shared contracts.** Using shared contracts is the only way judges will compare results.

*Source: Steve (April 1, private ticket)*

### ❓ Q: How does the Risk Router actually execute trades?

The hackathon page says "DEX execution via a whitelisted Risk Router contract (Uniswap-style routers)," but the template's RiskRouter only validates intents and emits events - it has no DEX integration, no swap calls, and doesn't spend from the Vault. How are ERC-8004 trades supposed to execute on-chain?

**Unanswered** as of April 2. Related to the open questions about sandbox capital and contract addresses - Steve is consulting with the Surge team.

### ❓ Q: Does implementing just the Validation Registry + a strategy qualify for the ERC-8004 challenge?

**Unanswered** as of April 2.

*Source: Kai (April 2, #general)*

### ❓ Q: How do agents set their own risk params on the shared RiskRouter? `setRiskParams` requires contract owner.

**Unanswered** as of April 1. This may mean organizers set default params for all agents, or there's a different method.

---

## Sandbox Capital & Hackathon Vault

### ❓ Q: How do we claim funds from the Hackathon Capital Vault? What are the contract addresses?

Steve's initial answer: "The sandbox capital is tied to your agentId. Once you deploy your contracts and mint your ERC-721 agent identity, you'll get your AGENT_ID, and it will be a sub-account anchor in the HackathonVault."

**However, the concrete Vault address, Risk Router address, and step-by-step claiming process have NOT been provided.** Steve is consulting with the Surge team. The tutorial shows how to deploy your own, but shared hackathon instances are needed for judging.

*Source: Steve (March 31, April 1 private ticket)*

### ✅ Q: Is the sandbox capital (Hackathon Vault) different from Kraken paper trading capital?

**Yes, completely separate:**
- **Kraken paper trading** = local simulation with imaginary capital against real market data (configurable starting balance)
- **Hackathon Capital Vault** = on-chain funded sub-account for the ERC-8004 challenge

How to actually access the Vault sub-account remains unanswered.

*Source: Zofia / Kraken Mentors (April 1)*

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

### ✅ Q: When are mentors available?

Mentors operate in **CET timezone** (no specific hours). Not 24/7 support. Tag @Mentors in the Discord channel and wait for a response.

*Source: Zofia (March 31)*

### ✅ Q: How do I get mentor feedback on my concept?

Create a ticket in the #create-a-ticket channel with a clear description of your question. Tag @Mentors in the general chat. Note: mentor validation is **not mandatory** - it's for additional help.

**WARNING: Do NOT click on external "support" links in Discord. Scammers are active.** Only use the official #create-a-ticket channel.

*Source: Zofia, Dave McFly (March 31)*

---

## Summary

✅ **Answered: 10** | ⚠️ **Partial: 3** | ❓ **Open: 8**

### Open Questions

| # | Question | Status |
|---|----------|--------|
| 1 | Shared contract addresses, sandbox capital, and how Risk Router executes trades | Steve consulting Surge team |
| 2 | Does Validation Registry + strategy alone qualify for ERC-8004? | Unanswered |
| 3 | PnL ranking method (absolute vs %) and minimum capital | Waiting on lablab/Surge ruling |
| 4 | How agents set RiskRouter params (`setRiskParams` requires owner) | Unanswered |
| 5 | Do all team members need Surge accounts? | Unanswered |
| 6 | How does "build in public" prize work with late Surge registration? | Unclear |
| 7 | Can teams use their own data sources (broker APIs)? | Unanswered |
| 8 | Social engagement measurement (Kraken) | Unanswered |
