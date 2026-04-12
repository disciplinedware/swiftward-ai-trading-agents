# MCP Servers - Reference

7 MCP servers. 6 for agents, 1 for operators. All JSON-RPC 2.0 over HTTP. Agent identity via `X-Agent-ID` header.

## Overview

| MCP | Prefix | Audience | Tools | Status | Spec |
|-----|--------|----------|-------|--------|------|
| Trading | `trade/` / `alert/` | Agent | 13 | Implemented | [trading.md](trading.md) |
| Market Data | `market/` | Agent | 8 core + 3 PRISM | Implemented | [market-data.md](market-data.md) |
| News | `news/` | Agent | 6 | Implemented | [news.md](news.md) |
| Files | `files/` | Agent | 8 | Implemented | (spec below in this file) |
| Code | `code/` | Agent | 1 | Implemented | [code-sandbox.md](code-sandbox.md) |
| Polymarket | `polymarket/` | Agent | 2 | Implemented | (see `golang/internal/mcps/polymarket/`) |
| Risk | `risk/` | Operator | 4 | Implemented | - |

Agents connect via MCP Gateway (`swiftward-server:8095/mcp/*`) - policy evaluation on every call.
Direct backend (`trading-server:8091/mcp/*`) - dev/testing only.

Design rationale: [../plans/completed/files-and-code-mcp.md](../plans/completed/files-and-code-mcp.md)

---

## Trading MCP (`trade/` + `alert/`)

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `trade/submit_order` | `pair`, `side`, `value` | `reason`, `stop_loss`, `take_profit` | `status`, `price`, `fill.qty`, `decision_hash`, `reason` |
| `trade/estimate_order` | `pair`, `side`, `value` | - | `price`, `estimated_qty`, `fee`, `portfolio_impact` |
| `trade/get_portfolio` | - | - | `cash`, `positions[]`, `portfolio.value`, `fill_count` |
| `trade/get_portfolio_history` | - | `limit` | `snapshots[]` (equity curve) |
| `trade/get_history` | - | `limit`, `pair`, `status` | `trades[]` with decision hashes |
| `trade/get_limits` | - | - | `portfolio.value`, `largest_position_pct`, `halted` |
| `trade/heartbeat` | - | - | `portfolio.value` (forces recompute) |
| `trade/set_conditional` | `pair`, `condition`, `value` | `side`, `qty_pct`, `note` | `alert_id` (software SL/TP/price alert) |
| `trade/cancel_conditional` | `alert_id` | - | `success` |
| `trade/set_reminder` | `when`, `note` | - | `alert_id` (time-based wake) |
| `trade/end_cycle` | - | `note` | `success` (closes trading cycle, updates peak equity) |
| `alert/triggered` | - | - | `alerts[]` (consume triggered alerts) |
| `alert/list` | - | - | `alerts[]` with distance-to-trigger |

`side`: `buy` or `sell` only. `value` in USD.

---

## Market Data MCP (`market/`)

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `market/get_prices` | `markets[]` | - | `prices[]` with bid/ask/last/volume/change_24h_pct |
| `market/get_candles` | `market`, `interval` | `limit`, `end_time`, `indicators[]`, `save_to_file` | candle data inline OR `{saved_to, rows, columns}` |
| `market/get_orderbook` | `market` | `depth` | `bids[]`, `asks[]`, `spread`, `imbalance` |
| `market/list_markets` | - | `quote`, `sort_by` | `markets[]` |
| `market/get_funding` | `market` | `limit` | `current_rate`, `annualized_pct`, `signal`, `history[]` |
| `market/get_open_interest` | `market` | - | `open_interest`, `oi_change_*_pct`, `long_short_ratio` |
| `market/set_alert` | `market`, `condition`, `value` | `window`, `note` | `alert_id` |
| `market/cancel_alert` | `alert_id` | - | `success` |

Optional PRISM tools (only if `PRISM_ENABLED=true`): `market/get_fear_greed`, `market/get_technicals`, `market/get_signals`.

`interval`: `1m`, `5m`, `15m`, `1h`, `4h`, `1d`.
`condition`: `above`, `below`, `change_pct`, `volume_spike`, `funding_threshold`, `oi_change_pct`.
`indicators[]`: `rsi_<period>`, `ema_<period>`, `sma_<period>`, `macd`, `bbands`, `atr_<period>`, `vwap`.
`save_to_file=true`: writes CSV to `/data/workspace/{agent-id}/market/{market}_{interval}.csv`, visible in sandbox as `/workspace/market/{market}_{interval}.csv`. Returns path only - keeps CSV data out of LLM context.

---

## Files MCP (`files/`)

**Replaces Memory MCP.** Unified filesystem for the agent's entire workspace. Follows Claude Code's approach - memory is just files at a path convention, not a separate MCP.

Root: `/data/workspace/{agent-id}/`. Paths are relative to this root.

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `files/read` | `path` | `offset`, `limit` | `content`, `size_bytes`, `total_lines`, `truncated` |
| `files/write` | `path`, `content` | - | `path`, `size_bytes` |
| `files/edit` | `path`, `old_text`, `new_text` | `replace_all` | `replacements` |
| `files/append` | `path`, `content` | - | `path`, `size_bytes` |
| `files/delete` | `path` | `recursive` | `deleted` |
| `files/list` | - | `path`, `recursive` | `entries[]` with name/path/is_dir/size/modified_at |
| `files/find` | `pattern` | `path` | `files[]` sorted by modification time (newest first) |
| `files/search` | `pattern` | `path`, `glob`, `case_insensitive`, `context_lines`, `output_mode`, `max_results` | `matches[]` or `files[]` or counts |

`files/edit` errors if `old_text` not found - read file first, then edit.
`files/find` uses glob patterns: `*.csv`, `market/*_1h.csv`, `**/*.py`.
`files/search` output_mode: `"content"` (default), `"files_only"`, `"count"`.

Memory convention - platform auto-injects these paths every cycle:
- `memory/MEMORY.md` - always in LLM context
- `memory/sessions/{today}.md` - today's session log
- `memory/sessions/{yesterday}.md` - yesterday's log

---

## Code MCP (`code/`)

Python execution only. One persistent container per agent (`trading-sandbox-<agent_id>`). Variables, imports, and dataframes survive between calls via pickle round-trip. Idle timeout: 30 min.

Pre-installed: `pandas`, `numpy`, `scipy`, `matplotlib`, `pandas-ta`, `scikit-learn`, `statsmodels`, `plotly`, `requests`.

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `code/execute` | `code` OR `file` | `timeout` (default 30s, max 120s) | `stdout`, `stderr`, `exit_code`, `duration_ms` |

### Two execution modes

`code/execute` accepts **either** `code` (inline Python) or `file` (path to a `.py` script) — mutually exclusive:

1. **`code=...` (inline).** Python text is sent directly to the sandbox REPL via HTTP. The sandbox executes it. If the Python needs to read files, those files must exist inside the sandbox container's filesystem — see "Workspace mount" below.

2. **`file=scripts/foo.py`.** Trading-server reads the `.py` script from **its own** `/data/workspace/<agent_id>/<file>` (via `os.ReadFile` in the Go process), then sends the script text to the sandbox REPL as if it were inline code. The file never needs to be inside the sandbox — it's loaded by the Go process and streamed as a string.

   Path accepts the sandbox-absolute form (`/workspace/scripts/foo.py`) or the agent-relative form (`scripts/foo.py`); both resolve to the same host file. Only `.py` scripts are allowed (to load CSVs, use `code=` with `pd.read_csv(saved_to)`).

### Workspace mount

When `HOST_WORKSPACE_PATH` is set in the trading-server's env, the sandbox is started with `-v $HOST_WORKSPACE_PATH/<agent_id>:/workspace`. Files that trading-server writes at `/data/workspace/<agent_id>/...` are then visible inside the sandbox at `/workspace/...`. This is how inline `code=` Python can do `pd.read_csv('/workspace/market/ETH-USD_1h.csv')`.

**`HOST_WORKSPACE_PATH` must be an absolute host path** — Docker's daemon resolves the `-v SRC:DST` source from the host's filesystem perspective, not from inside any container. The `Makefile` auto-sets this via `HOST_WORKSPACE_PATH ?= $(PWD)/data/workspace` at make invocation time, so any `make up` / `make local-up` run gets it for free on any machine.

**Gotcha: direct `docker compose up -d` bypasses make and does NOT set the env var.** If you recreate trading-server outside of `make`, pass the var explicitly or the sandbox will start without a mount and inline `code=` Python that reads files will `FileNotFoundError`:

```bash
# Wrong — no mount, files invisible in sandbox:
docker compose up -d --force-recreate trading-server

# Right:
HOST_WORKSPACE_PATH="$(pwd)/data/workspace" docker compose up -d --force-recreate trading-server
# Or just:
make up PROFILES=delta
```

### Which mode each agent uses

- **Claude agents (alpha, gamma)** — mostly `file=scripts/foo.py` mode. Claude authors a script via Write tool (lands on the host via gamma-claude's own `./data/workspace/agent-gamma-claude` compose bind mount), then calls `code/execute file=scripts/foo.py` to run it. Also uses Claude's native `Read`/`Bash` tools to inspect CSVs without ever touching the sandbox filesystem.
- **Go LLM agent (delta)** — uses `code=` mode with inline Python that does `pd.read_csv(saved_to)` where `saved_to` comes from `market/get_candles?save_to_file=true`. This is the path that **requires** the sandbox bind-mount to be active.

For system operations use `subprocess` inside `code/execute`: install packages with `subprocess.run(['pip','install','pkg'])`, run shell commands with `subprocess.run([...], capture_output=True)`. No separate install tool.

---

## News MCP (`news/`)

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `news/search` | `query` | `markets[]`, `filter`, `kind`, `date_from`, `date_to`, `limit` | `articles[]` with title/source/url/published_at/sentiment/markets/kind, `count`, `source` |
| `news/get_latest` | - | `kind`, `markets[]`, `filter`, `limit` | `articles[]` sorted by recency, `count`, `source` |
| `news/get_sentiment` | `query` | `markets[]`, `period` | `query`, `score` (-1 to 1), `sentiment`, `article_count`, `key_themes`, `period`, `source` |
| `news/get_events` | - | `markets[]`, `type`, `days` | `events[]` with title/type/date/impact_level/details/market, `count`, `source` |
| `news/set_alert` | `query`, or `markets[]` + `keywords[]` | `note` | `alert_id` (keyword / market news alert) |
| `news/get_triggered_alerts` | - | - | `alerts[]` of fired news alerts |

---

## Polymarket MCP (`polymarket/`)

Read-only prediction markets. No auth, no trading. Exposed via MCP Gateway with guardrails disabled.

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `polymarket/search_markets` | - | `query`, `category`, `sort_by`, `limit` | events grouped with inline markets, odds, 24h volume, liquidity, close time |
| `polymarket/get_market` | `market_id` | - | full deep-dive: description, resolution criteria, odds, order book snapshot, volumes, fees, sibling markets |

---

## Risk MCP (`risk/`)

Operator API only. No `X-Agent-ID` scoping - sees all agents.

| Tool | Required | Optional | Returns |
|------|----------|----------|---------|
| `risk/halt_agent` | `agent_id` | `reason` | `success`, `halted_at` |
| `risk/resume_agent` | `agent_id` | - | `success`, `resumed_at` |
| `risk/get_agent_status` | `agent_id` | - | `halted`, `portfolio.value`, `fill_count`, `last_active` |
| `risk/list_agents` | - | - | `agents[]` with status summary |

---

## Error Format

```json
{"error": {"code": -32602, "message": "market is required"}}
```

Common app-level codes: `AGENT_HALTED`, `REJECTED_BY_POLICY`, `MARKET_NOT_FOUND`, `RATE_LIMITED`, `OLD_TEXT_NOT_FOUND`.

## Related Docs

- [../architecture/overview.md](../architecture/overview.md) - System diagram, data flows, volume layout
- [../models/agent-prompt.md](../models/agent-prompt.md) - How agents use these tools
- [../plans/completed/files-and-code-mcp.md](../plans/completed/files-and-code-mcp.md) - Files + Code MCP design plan
- [../decisions/trade-db.md](../decisions/trade-db.md) - Trade DB design
