# Agentic Hedge Fund: Evolutionary Capital Management

## Concept
Instead of using a single complex trading algorithm, the investor's capital ($1,000,000) is distributed across **100 independent AI agents**. Each agent starts with 1% of the portfolio ($10,000) and operates within its own unique strategy (prompt) and model (GPT-4o, Claude 3.5, Llama 3, etc.).

## System Architecture

### 1. Execution Layer (The 100 Agents)
Each agent runs in an isolated MCP session:
- Has its own balance and trade history.
- Makes decisions based on market data (CSV) and news (DB).
- Time flows synchronously for all agents under the Orchestrator's control.

### 2. Algorithmic Rebalancing Layer (The Accountant)
Once per trading cycle (day/week), capital weights are recalculated based on mathematical metrics:
- **PnL-Weighting**: Profitable agents receive more funds for the next cycle.
- **Sharpe Ratio**: Not just profit is considered, but also risk (return volatility).
- **Drawdown Protection**: Agents that have lost more than 20% of their initial capital are automatically paused.

### 3. LLM Judge Layer (The Investment Committee)
A specialized model (e.g., GPT-4o with long context) analyzes the "thought" logs and actions of each agent:
- **Consistency Score**: Does the trade match the analysis performed? (Protection against lucky guesses).
- **Strategy Validation**: Did the agent find a real market inefficiency, or just got lucky?
- **Quality Filter**: The judge can veto a capital increase for a bot if its logic appears flawed or excessively risky.

### 4. Evolutionary Cycle (The Lab)
The system continuously self-improves on Darwinian principles:
1. **Selection**: The worst 10% of agents (by combined Score from the Algorithm and Judge) are removed.
2. **Cloning**: The top 10% of agents are cloned.
3. **Mutation (LLM Creator)**: Prompt variations are created for clones. For example, if "Quant-analyst" won, the system creates versions of it with different indicator focuses.

## Benefits for the Investor

1. **Unprecedented diversification**: Risk is spread across 100 different "brains" and strategies.
2. **Transparency**: Every trade is justified by a text log of the agent's "thoughts", verified by the Judge.
3. **Adaptability**: The system figures out which models and prompts perform best in the current market (bull, bear, or sideways) and reallocates capital accordingly.
4. **Scalability**: The Rails + GoodJob architecture makes it easy to scale the number of agents to 1000 or more.

## Tech Stack
- **Core**: `solid_loop` (agent orchestration).
- **Transport**: MCP (trading and analytics tools).
- **Data**: Binance OHLC Mirror + Telegram News Engine.
- **Management**: Custom Rebalancing Service (Hybrid: Ruby + LLM Judge).
