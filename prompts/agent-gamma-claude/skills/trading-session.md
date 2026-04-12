---
description: Full multi-agent trading session pipeline with screening funnel
---

Run the complete trading session pipeline as described in your CLAUDE.md:

1. Load: portfolio, list_markets (all USD pairs), history, limits, strategy file, scripts, drawdown check
2. Screen: all available USD markets, classify regime, select top 3-5 for full analysis
3. Analyze: spawn 3 analyst subagents in parallel on selected markets
4. Debate: spawn 2 researcher subagents in parallel with analyst reports
5. Decide: synthesize reports, volatility-based sizing, correlation guard, regime adaptation
6. Execute: if decided, accept Swiftward policy decisions
7. Record: write mandatory session log to `analysis/session-{N}-{date}.md` (format in CLAUDE.md), set/update alerts, note insights in memory

End with the appropriate session completion signal as defined in CLAUDE.md.
