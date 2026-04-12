---
description: Deep single-market analysis on demand
---

Run a comprehensive deep analysis on the specified market.

If no market is specified, ask which market to analyze.

Steps:
1. Fetch current price: `market/get_prices` for the specified market (and BTC-USD for context)
2. Spawn 3 analyst subagents IN PARALLEL (single message) using the standard input block format from CLAUDE.md:
   - Technical Analyst: `.claude/agents/technical-analyst.md`
   - Sentiment Analyst: `.claude/agents/sentiment-analyst.md`
   - Market Structure Analyst: `.claude/agents/market-structure-analyst.md`
3. After all 3 return, spawn 2 researcher subagents IN PARALLEL (single message), passing all analyst reports:
   - Bull Researcher: `.claude/agents/bull-researcher.md`
   - Bear Researcher: `.claude/agents/bear-researcher.md`
4. Run your own Python quantitative analysis on top (fetch additional data if needed - 1d candles, longer history, cross-market correlation)
5. Produce a detailed written analysis with specific entry/exit levels and scenarios
6. Save the analysis to `analysis/{market}-{date}.md`

Output your findings.
