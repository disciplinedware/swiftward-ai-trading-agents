# ruby

Ruby subsystem: **Agents Arena** (Solid Loop Trading) — a Rails-based backtesting platform for agentic trading loops. Runs batches of agent runs over historical data with virtual time, collects equity curves, and reuses the same agent code for live trading through the Swiftward gateway. Built on the [SolidLoop](https://github.com/Ruslan/solid_loop) gem, which owns loop state, messages, and tool calls as first-class records.

The platform ships with two agent implementations.

## What's inside

- **Solid Loop Trading** (`agents/solid_loop_trading/`) — the backtesting platform: batch runner, turn-by-turn orchestrator, virtual-time session ledger, background wake-up job, PnL presenter. Exposes a Rails dashboard/API and Sidekiq-style background workers.
- **TradingAgent** — backtest-native agent implementation. Runs inside the platform under virtual time against the trading + sandbox MCPs.
- **SwiftwardTradingAgent** — live-trading variant of the same agent. Routes through the Swiftward policy gateway and adds market, news, files, and code MCPs.

## Key files

### Platform — backtesting & orchestration

- [`agents/solid_loop_trading/app/services/trading_orchestrator_service.rb`](agents/solid_loop_trading/app/services/trading_orchestrator_service.rb) — turn-by-turn orchestrator: advances virtual time, compacts old messages, finalizes the simulation.
- [`agents/solid_loop_trading/app/services/agent_batch/create.rb`](agents/solid_loop_trading/app/services/agent_batch/create.rb) — batch spawner: instantiates an `AgentRun` + `SolidLoop::Loop` for each scenario × attempt.
- [`agents/solid_loop_trading/app/jobs/agent_orchestrator_job.rb`](agents/solid_loop_trading/app/jobs/agent_orchestrator_job.rb) — background job: polls waiting agents, wakes them on alerts or heartbeats, feeds the next turn.
- [`agents/solid_loop_trading/app/models/trading_session.rb`](agents/solid_loop_trading/app/models/trading_session.rb) — virtual trading ledger: syncs filled orders, manages time offset, tracks portfolio balance.

### Agents

- [`agents/solid_loop_trading/app/agents/trading_agent.rb`](agents/solid_loop_trading/app/agents/trading_agent.rb) — backtest agent: MCP session wiring (trading + sandbox), system prompt, LLM provider, session initialization hook.
- [`agents/solid_loop_trading/app/agents/swiftward_trading_agent.rb`](agents/solid_loop_trading/app/agents/swiftward_trading_agent.rb) — live-trading variant: routes through the Swiftward guarded gateway and adds market, news, files, and code MCPs.
