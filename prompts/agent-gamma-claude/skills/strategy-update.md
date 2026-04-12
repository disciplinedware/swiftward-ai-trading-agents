---
description: Update trading strategy based on performance data - analyze what worked, propose and write changes
---

Run a strategy update session. Review performance, compare against current strategy, propose and write concrete changes.

Steps:

1. **Load current strategy**: `files/read` on `strategies/current-strategy.md`
   - If file does not exist: note that no strategy file exists yet, you will create the baseline from memory

2. **Run portfolio analysis** (same logic as portfolio-review skill):
   - `trade/get_history` - full history
   - `trade/get_portfolio` - current state
   - Using Python, compute:
     - Win rate overall and per-market
     - Avg win / avg loss ratio per market
     - Best and worst performing times of day (bucket trades by hour UTC)
     - Performance by position size tier (small <5%, medium 5-10%, large >10%)
     - Performance after policy rejections (did the next trade work out?)
     - Consecutive win/loss streaks - any patterns?
     - Max drawdown and when it occurred

3. **Compare against current strategy**:
   - What markets does the strategy prioritize? Do the results support that?
   - What position sizing does the strategy use? Are the base%, vol_adjust, and conviction_mult parameters calibrated correctly? Is the 2-15% range appropriate?
   - What conditions does the strategy require for entry? Are those conditions actually predictive?
   - What conviction thresholds does the strategy use? Are they calibrated correctly?
   - Any alerts the strategy relies on? Cross-reference with alert performance

4. **Identify changes** - be specific, not vague:
   - Good: "Increase SOL-USD base size from 5% to 7% - SOL win rate is 71% vs 52% for ETH"
   - Bad: "Consider focusing more on better performing markets"
   - For each proposed change: state the evidence, the current setting, the proposed setting, and the expected impact

5. **Backtest proposed changes** before committing:
   - For each proposed rule change, fetch 30 days of 1h candles for the relevant market(s)
   - Run Python to compare old params vs new params: "old rule caught X/Y winning setups, new rule catches Z/Y"
   - Only promote changes where the new params backtest better
   - Save/update reusable backtest scripts in `/workspace/scripts/` so future sessions can use them
   - Also maintain/improve: indicator library, pair scanner, portfolio analyzer scripts

6. **Write updated strategy** to `strategies/current-strategy.md` via `files/write`:
   - Keep the structure of the existing file if it exists
   - Mark changed sections clearly with `[UPDATED {date}]` inline
   - If no prior file: create a clean strategy document with all parameters derived from the data
   - Include per-market bias and sizing adjustments
   - Include current regime assessment and what flips it
   - Include correlation notes between tracked pairs

7. **Write changelog entry** to `strategies/strategy-log.md` via `files/write` (append mode if file exists):
   ```
   ## {date} - Strategy Update
   **Trigger**: [manual / performance-driven / after N sessions]
   **Key metrics at time of update**: win rate X%, equity Y, sessions Z
   **Changes**:
   - [change 1: what changed and why]
   - [change 2: ...]
   **Hypothesis**: [What do you expect to improve and by how much?]
   ```

8. **Update memory** with the key strategic shifts so future sessions pick them up immediately

Output a summary of what changed and why.

Do NOT change the strategy based on fewer than 10 trades. If history is too short, write the current
baseline to the file without changes and note "insufficient data for strategy update - need N more trades".
