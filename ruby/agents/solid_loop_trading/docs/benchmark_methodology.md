# Trading Benchmark Methodology

## Goal
Objective comparison of the performance of various LLM models and trading strategies (prompts) on historical data with high event density.

## Run Scenario
1. **Start point**: March 1, 2026.
2. **Starting capital**: 100,000 USDT.
3. **Time step**: The orchestrator advances time by 1 hour after each agent response.
4. **Duration**: 31 days (all of March).
5. **Final point**: Call `finish_trade` at the end of the month.

## Evaluation Metrics
1. **PnL (Profit and Loss)**: Net profit in USDT after closing all positions.
2. **Max Drawdown**: Maximum drop in portfolio equity during the test.
3. **News Sensitivity**: Speed of reaction to critical news (e.g., events on March 23).
4. **Token Efficiency**: Ratio of profit to number of tokens consumed (cost of agent "thinking").

## Rules for the Agent
- The agent must check the portfolio via `get_portfolio` before trading.
- The agent must account for a 0.1% commission and spread.
- The agent may use both market and limit orders.
