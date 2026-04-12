# Code Sandbox MCP

> **Status**: ✅ Shipped
> **Package**: `golang/internal/mcps/codesandbox/`
> **Image**: `sandbox-python:local` (locally built) or `ghcr.io/disciplinedware/ai-trading-agents/sandbox-python:latest`
> **Endpoint**: `POST /mcp/code`

## What was built

A per-agent persistent Python 3.12 Docker container that executes code and preserves state across calls via pickle-based subprocess serialization. Agents write analysis scripts, run backtests, compute indicators, and generate trading signals without polluting the LLM context - state persists across execution boundaries, market data flows through a shared workspace volume, and only results come back to the model.

## MCP tools

| Tool | Purpose | File:line |
|------|---------|-----------|
| `code/execute` | Execute Python inline or from a workspace file in the agent's sandbox; state persists across calls | `service.go:390-458` |

Only one tool is exposed. That is intentional - the shape of the agent's work (load data → analyze → write results) is served by one universal call.

## Sandbox image

- **Base**: `python:3.12-slim`
- **Dockerfile**: `golang/internal/mcps/codesandbox/Dockerfile`
- **Pre-installed packages**: pandas, numpy, scipy, pandas-ta, scikit-learn, statsmodels, matplotlib, plotly, requests
- **Runtime `pip install`** of additional packages is allowed from inside `code/execute`
- **Working directory**: `/app` (holds `repl.py`)
- **Port**: `8099` (internal REPL server)

## Per-agent container lifecycle

1. **Creation** (`service.go:186-237`): first `code/execute` for `{agent-id}` triggers `getOrCreateContainer`. Concurrent calls for the same agent are serialized via a `creating[agentID]` sentinel and `createCond`, so exactly one container is spawned even under a thundering herd.
2. **Container name**: `trading-sandbox-{agent-id}` (note: the Docker image is `sandbox-python`, the container name prefix is `trading-sandbox`).
3. **Address resolution**: if `DOCKER_NETWORK` is configured, the handler reaches the REPL by container name on port 8099. Otherwise it inspects `docker port` output and reaches it via `localhost:{mapped_port}`.
4. **Startup** (`service.go:240-313`): `docker run` with the workspace bind mount (if `HOST_WORKSPACE_PATH` is set), then `waitForRepl()` polls `GET http://{host}:{port}/` until the REPL responds (~400 ms cold start).
5. **Reuse**: container stays alive for 30 minutes of inactivity (`IdleTimeout`).
6. **Cleanup**: a background goroutine evicts idle containers every 60 seconds (`service.go:115-126`).
7. **Crash recovery**: if a container stops unexpectedly, the next `code/execute` detects the stopped state and spawns a fresh one.

## Workspace

- **Host path**: `HOST_WORKSPACE_PATH/{agent-id}/`
- **Container path**: `/workspace/`
- **Shared with the Files MCP**: both services read and write the same mounted volume, so file writes from `files/write` are immediately visible from `code/execute`, and vice versa

Typical pattern inside sandbox code:

```python
import pandas as pd
df = pd.read_csv('/workspace/market/ETH-USDC_1h.csv')
# ...analysis...
df.to_csv('/workspace/output/analysis.csv')
```

## State persistence

The REPL server (`golang/internal/mcps/codesandbox/repl.py:38-138`) preserves state across calls:

1. Before each execution, snapshot the current `_globals` dict, filter to picklable values
2. Write the pickle to a temp file
3. Spawn a subprocess that loads the pickle, executes the user code against that environment, and writes the new globals back
4. The parent merges changed / new variables back into `_globals`

This means variables, imports, loaded DataFrames, and function definitions survive across calls to `code/execute` **within the same container**. Session isolation is per-agent: one container = one agent's Python state.

Timeout enforcement (`repl.py:106-116`) uses `subprocess.run(timeout=)` which sends `SIGKILL`, so even code stuck inside a C extension (numpy, scipy) is terminated reliably.

## Key files

- `golang/internal/mcps/codesandbox/service.go` - MCP handler, container lifecycle, tool dispatch
- `golang/internal/mcps/codesandbox/repl.py` - Python REPL server, pickle serialization, code execution
- `golang/internal/mcps/codesandbox/Dockerfile` - Python 3.12 image definition
- `golang/internal/mcps/codesandbox/service_test.go` - unit tests for validation and Docker port parsing
- `golang/internal/mcps/codesandbox/service_integration_test.go` - integration tests (behind `integration` build tag)

## Tests

**Unit** (`service_test.go`):
- Agent ID validation (no path traversal, limited character set)
- Docker port parsing edge cases

**Integration** (`service_integration_test.go`, requires Docker and `-tags integration`):
- Simple code execution and stdout capture
- State persistence across two sequential calls
- Timeout enforcement (process killed after `timeout` seconds)
- Syntax errors return `exit_code=1` with traceback
- Pandas works end to end
- Runtime `pip install`
- Concurrent calls for the same agent serialize correctly

## Configuration

Via koanf, env prefix `TRADING__CODE_MCP__`:

- `SANDBOX_IMAGE` - Docker image (default `ghcr.io/disciplinedware/ai-trading-agents/sandbox-python:latest`)
- `IDLE_TIMEOUT` - container idle eviction threshold (default `30m`)
- `STARTUP_TIMEOUT` - max wait for REPL readiness (default `5m`)
- `WORKSPACE_DIR` - host path prefix for the bind mount
- `HOST_WORKSPACE_PATH` - required when running inside Docker-in-Docker
- `DOCKER_NETWORK` - Docker network name, empty means host port mapping

## Notes

- **Isolation**: Docker process and filesystem namespace per agent. No gVisor or Firecracker; the threat model is "prevent the LLM from reading host secrets", not "defend against a determined attacker". Containers can reach PyPI for `pip install`.
- **Timeout lives in Python, not Docker**: `subprocess.run(timeout=)` is the enforcement point. The LLM can override `timeout` up to 120 seconds.
- **Pickle constraints**: non-picklable objects (open files, sockets, lambdas defined in the user call) are dropped when state is snapshotted. For persistent non-picklable state, write to files instead.
- **Why not Jupyter**: Jupyter adds kernel manager complexity and WebSocket overhead. Pickle-based subprocess is simpler, faster to cold-start, and sufficient for the agent use case.
- **Why one tool instead of many**: `code/execute` handles inline code and file-based code equally well. Adding `read_state`, `reset_state`, `install_package` would fragment the agent's mental model with no real benefit.
