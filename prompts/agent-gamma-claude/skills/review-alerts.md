---
description: Audit alert quality - hit rate, false alarms, outcome analysis, recommendations
---

Run a full alert quality audit.

Steps:
1. Get trade history: `trade/get_history` (last 30 days)
2. Get current portfolio: `trade/get_portfolio`
3. Get current alerts: `alert/list` for all active alerts and conditional orders (or read from memory if tools unavailable)
4. Pull market candles for markets with active alerts or recent trades: `market/get_candles` with `interval=1h`, `limit=100`, `save_to_file=true` for price history context
5. Using Python, correlate alerts against trades:
   - For each trade, check if an alert fired within the 2 hours prior - mark as "alert-triggered"
   - For each alert that fired, check if a trade followed within 4 hours - mark as "acted on"
   - For significant price moves (>3% in 4h) with no prior alert: flag as "missed signal"
6. Compute per-alert-type metrics:
   - Hit rate: alerts that led to a trade / total alerts fired
   - Action rate: alerts acted on / alerts fired
   - Avg PnL after trigger: mean realized PnL for trades that followed an alert (use 24h forward return if trade still open)
   - False alarm rate: alerts fired where price moved against the signal within 4h
7. Read your memory for any notes about specific alerts being noisy or useful
8. Write analysis to `analysis/alert-review-{date}.md` via `files/write` with:
   - Raw counts and computed metrics table
   - List of missed signals with price context
   - List of false alarms with market context
   - Summary table: `alert type | fired | acted | hit rate | avg PnL | recommendation`
   - Recommendations section: concrete parameter changes (tighten threshold, widen window, remove entirely)
9. Update memory with key findings and any alert changes you decide to make immediately

Recommendation categories:
- **keep**: hit rate > 60%, avg PnL positive
- **tune**: hit rate 30-60% or avg PnL near zero - specify what to change
- **remove**: hit rate < 30% AND avg PnL negative, or never fired in 30 days

Output a short summary table to the session.
