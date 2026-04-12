# Agent Workspace

> **Status**: ✅ Shipped
> **Mount point in containers**: `/workspace`
> **Host path**: `./data/workspace/{agent-id}/` (configurable via `HOST_WORKSPACE_PATH` for DinD)

## What was built

A unified, persistent workspace per agent. Every tool that touches files - Files MCP, Code Sandbox MCP, and the Claude Code agent - reads and writes the same subtree, so a CSV written by `market/get_candles` with `save_to_file=true` is immediately readable from `code/execute` and from `files/read`. No separate sync step, no path translation, no surprises.

## Layout

Each agent sees only its own subtree, rooted at `/workspace/` inside containers and `./data/workspace/{agent-id}/` on the host.

```
/workspace/{agent-id}/
├── market/                 # Market Data MCP CSVs (save_to_file=true)
│   ├── ETH-USDC_1h.csv
│   └── BTC-USDC_4h.csv
├── scripts/                # Agent-written Python scripts
│   └── analysis.py
├── output/                 # Code Sandbox results
│   └── results.csv
├── memory/
│   └── MEMORY.md          # Agent persistent memory
├── CLAUDE.md              # Auto-loaded by Claude Code on session start
└── ...                     # Agent is free to create its own structure
```

## Integration

- **Files MCP** (`/mcp/files`) - structured access with `read`, `write`, `edit`, `append`, `delete`, `list`, `find`, `search`. Per-agent path sandboxing via `safePath` and `X-Agent-ID` header validation. 10 MB cap on single writes.
- **Code Sandbox MCP** (`/mcp/code`) - per-agent Python container with `/workspace/` bind-mounted. Python reads CSVs with `pd.read_csv('/workspace/market/...')` and writes results to `/workspace/output/`. State persists across `code/execute` calls via pickle.
- **Claude Code agent** - container mounts the same subtree. Uses Files MCP for writes through the gateway, uses `/workspace/CLAUDE.md` as auto-loaded memory, and uses Code Sandbox for computation.

## How `save_to_file` ties it together

`market/get_candles` with `save_to_file=true` writes CSV to `{workspace}/{agent-id}/market/{market}_{interval}.csv` and returns only the path. The agent then calls `code/execute`:

```python
import pandas as pd
df = pd.read_csv('/workspace/market/ETH-USDC_1h.csv')
# ...analysis...
df.to_csv('/workspace/output/analysis.csv')
```

The CSV never enters the LLM context window. This is what makes deep backtests feasible from inside a Claude session.

## Key files and configuration

- `golang/internal/config/config.go` - `FilesMCPConfig.RootDir`, `CodeMCPConfig.HostWorkspacePath`
- `compose.yaml` - `trading_data` Docker volume mounted at `./data` on the host
- `docker/claude-agent/entrypoint.sh` - mounts `/workspace` and `/home/app/.claude` in the Claude Code container
- `golang/internal/mcps/files/service.go` - path validation (`safePath`, `checkSymlinkEscape`)
- `golang/internal/mcps/codesandbox/service.go` - container bind-mount logic

## Notes

- **Isolation**: each agent has its own subtree. No agent can read or write outside its own directory thanks to `safePath` and symlink escape detection.
- **Docker-in-Docker**: when the Code Sandbox is run from a container (instead of the host), `HOST_WORKSPACE_PATH` provides the absolute host path needed to bind-mount the correct directory into the sandbox child container.
- **Separation from platform memory**: Swiftward state and agent counters are in Postgres, not the workspace. The workspace is for *agent-generated* artifacts (notes, scripts, CSVs, results).
- **10 MB write cap**: prevents an agent from filling the disk via a runaway `files/write`. Code Sandbox writes are not capped but are bounded by the container's resource limits.
