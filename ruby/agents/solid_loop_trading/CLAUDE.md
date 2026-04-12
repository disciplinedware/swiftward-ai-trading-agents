# Solid Loop Trading — Ruby Rails Agent

Ruby/Rails backtesting and swarm agent. Runs at http://localhost:7175.
App code: `ruby/agents/solid_loop_trading/` (this directory, inside ai-trading-agents repo).

**SolidLoop gem** (framework): https://github.com/Ruslan/solid_loop
To study the gem source, clone it into a temp folder:
```bash
git clone https://github.com/Ruslan/solid_loop /tmp/solid_loop
```
Do not edit it in place — treat as read-only reference.

## Code layout

| What | Path |
|------|------|
| Models | `app/models/` |
| Agents | `app/agents/` |
| MCP tools | `app/services/mcp_tools/` |
| PnL presenter | `app/presenters/local_trading_session_presenter.rb` |
| DB migrations | `db/migrate/` |

## Key models

- **AgentRun** — one agent execution. Has `loop` (SolidLoop::Loop), no direct `pnl` field.
- **SolidLoop::Loop** — agent loop: `state` (JSONB), `cost`, `step_count`, `messages`.
- **TradingSession** — virtual trading env with ledger. Often 0 rows (sessions managed via MCP).
- **LedgerEntry** — accounting (trade/fill/reservation/deposit). Source of truth for equity.
- **LocalTradingSessionPresenter** — computes `initial_balance`, `total_equity_usdt`, `ledger_rows`.

## Docker containers

| Container | Purpose |
|-----------|---------|
| `solid-loop-trading-web-1` | Rails web (port 7175) |
| `solid-loop-trading-worker-1` | GoodJob background worker |
| `solid-loop-trading-db-1` | PostgreSQL |

## Running tests

Tests run **locally** (not in Docker) from the project directory:

```bash
cd ruby/agents/solid_loop_trading
bundle exec rspec                                   # all tests
bundle exec rspec spec/presenters/                  # presenters only
bundle exec rspec spec/path/to/file_spec.rb         # single file
```

RSpec is already configured locally — no Docker needed.

## Debugging with rails runner

```bash
# Quick check
docker exec solid-loop-trading-web-1 bin/rails runner "p AgentRun.count"

# Inspect AgentRun
docker exec solid-loop-trading-web-1 bin/rails runner "
run = AgentRun.find(10)
p run.attributes
p run.loop.state
p run.loop.cost
run.loop.messages.order(:created_at).each do |m|
  puts \"[#{m.role}] #{m.content.to_s[0..80]}\"
  m.tool_calls.each { |tc| puts \"  TOOL #{tc.function_name}: #{tc.result.to_s[0..60]}\" }
end
"
```
