---
description: Quick morning market brief (5-10 min) - overnight moves, positions, action items
---

Run a quick morning market brief. No trading. No full pipeline.

Note the current UTC time from your session context. If it is NOT between 07:00 and 09:00 UTC,
still proceed - a brief requested outside the window is still useful.

Steps:
1. **Market snapshot**: `market/list_markets` with `quote=USD`, `sort_by=volume` - all available USD pairs
2. **Sentiment**: `market/get_fear_greed` - current index (check memory for last recorded value to compute delta; if unavailable, omit delta)
3. **Funding rates**: `market/get_funding` for BTC (always) and any markets with >2% 24h change
4. **BTC regime data**: `market/get_candles` for BTC-USD: `interval=1h`, `limit=60`, `indicators=["ema_20", "ema_50"]`
5. **Overnight news**: `news/get_latest` - top 5 headlines from last 8 hours
6. **Macro events**: `news/get_events` - anything today or tomorrow that matters (FOMC, CPI, major unlocks)
7. **Positions**: `trade/get_portfolio` - for each open position, compute overnight PnL delta from last session price if recorded in memory
8. **Triggered alerts**: check `trade/get_history` for any `auto_executed: true` entries since your last session

Assemble the brief in this structure:

```
# Morning Brief - {date} {time} UTC

## Market Overview
| Market    | Price  | 24h Change | Funding | Notes |
|-----------|--------|------------|---------|-------|
[One row per tracked market. Highlight any >2% movers.]

Fear & Greed: {value} ({label}) [delta from last recorded: {delta} or "N/A"]

## Key Overnight Moves
- [List any market that moved >2% overnight with brief context]
- [If nothing significant: "Quiet overnight, no moves >2%"]

## News Headlines
- [Top 3-5 relevant headlines with brief note on market impact]
- [Flag any: protocol exploits, regulatory news, macro surprises]

## Upcoming Events
- [Today/tomorrow events: FOMC, CPI, major token unlocks, protocol upgrades]

## Position Status
| Market | Size | Entry | Current | PnL | Overnight | Alerts? |
|--------|------|-------|---------|-----|-----------|---------|
[One row per open position. "Alerts?" = any triggered overnight]

## Auto-Executed Trades
[List any stop-loss/take-profit that fired overnight, or "None"]

## Market Regime
Based on BTC EMA20 vs EMA50 (from step 4), Fear & Greed (step 2), and BTC funding (step 3):
- Classification: [trending-bull / trending-bear / range-bound / high-volatility]
- Key signals: [what drove the classification]
- Implication: [how trading session should adapt - sizing, stop width, setup type]

## Action Items
- [Concrete list: e.g. "ETH stop-loss tight at -8% - consider loosening if thesis intact"]
- [e.g. "BTC broke key level overnight - review thesis before next session"]
- [e.g. "No action needed - quiet night"]
```

Write the brief to `analysis/morning-{date}.md`.

Output the brief to the session.
