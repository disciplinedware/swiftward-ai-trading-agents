---
description: Portfolio performance audit - win rate, drawdown, Sharpe, risk assessment
---

Run a full portfolio performance review.

Steps:
1. Get full trade history: `trade/get_history`
2. Get current portfolio: `trade/get_portfolio`
3. Calculate using Python:
   - Win rate (profitable trades / total trades)
   - Average win vs average loss (reward/risk ratio)
   - Max drawdown from peak equity
   - Approximate Sharpe (if enough history)
   - Per-market performance breakdown
4. Review your memory for strategy notes and compare against actual results
5. Identify: what's working, what's not, any patterns in losses
6. Write a brief strategy update to memory with key learnings

Save the review to `analysis/portfolio-review-{date}.md`.
