# Trading Benchmark Platform (Backtrading Demo)

## System Overview
The platform is designed for testing and comparing trading strategies managed by LLM agents. The system uses the `solid_loop` gem for agent orchestration and provides a set of trading tools via a built-in MCP controller.

## "Self-Hosted MCP" Architecture
Unlike classic external MCP servers, the trading tools are implemented inside this same Rails application:
- **Client**: `solid_loop` (inside a `GoodJob` worker) makes HTTP requests to the tool.
- **Server**: A Rails controller (`/mcp/`) that handles JSON-RPC calls and executes trading business logic.
- **Environment**: Depending on the environment, the tool server URL is configured as `http://localhost:3000/mcp` or `http://app-host:3000/mcp`.

## Tool Set (Trading Tools)

### 1. `get_historical_data`
- **Description**: Returns an array of candles (OHLCV) for the specified period.
- **Parameters**: `symbol`, `interval`, `start_time`, `end_time`.

### 2. `execute_trade`
- **Description**: Opens a position (long/short).
- **Parameters**: `symbol`, `side`, `amount`, `price`.

### 3. `get_portfolio_state`
- **Description**: Returns the current balance and open positions.

### 4. `finish_trade` (Critical Tool)
- **Description**: Closes all positions and ends the trading session.
- **Result**: Returns the final **PnL (Profit and Loss)**.
- **Benchmark Integration**: The PnL value from this tool is automatically recorded as the primary success metric (`score`) in the benchmark.

## Benchmarking Process
1. **Preparation**: An `AgentBenchmark` is created with the trading agent configuration.
2. **Launch**: `solid_loop` starts parallel sessions (attempts).
3. **Loop**: The agent analyzes data via `get_historical_data` and executes trades.
4. **Finalization**: The agent must call `finish_trade()`.
5. **Aggregation**: The system collects PnL across all attempts and calculates:
    - **Total PnL**: Total profit/loss.
    - **Win Rate**: Percentage of profitable sessions.
    - **Sharpe Ratio** (optional): Risk assessment.
    - **Agent Efficiency**: Ratio of PnL to tokens consumed / time spent.

## Benefits of the Approach
- **Realism**: The agent operates under constrained API conditions, like a real trading bot.
- **Observability**: All trades and agent reasoning are logged in `SolidLoop::Event` and `AgentEvent`.
- **Scalability**: Up to 10 different models can be tested simultaneously on the same historical data.
