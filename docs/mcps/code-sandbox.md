# Code Sandbox MCP (`code/`)

One persistent Docker container per agent. The image is `sandbox-python` (Python 3.12); containers are named `trading-sandbox-{agent-id}`. State (variables, imports, dataframes) persists across calls within a session via pickle round-trip. Sandbox containers are started on first `code/execute` call (~400 ms cold start), then kept alive for 30 minutes of idle time.

**Endpoint**: `POST /mcp/code`

**Pre-installed**: `pandas`, `numpy`, `scipy`, `matplotlib`, `pandas-ta`, `scikit-learn`, `statsmodels`, `plotly`, `requests`.

Install additional packages via subprocess inside `code/execute`: `subprocess.run(['pip', 'install', 'pkg'], capture_output=True)`. No separate install tool - subprocess is more flexible.

**Workspace**: Container mounts the agent's workspace at `/workspace/` (same `trading_data` volume as Files MCP). Python can read/write files there directly alongside Files MCP. Market Data CSVs are at `/workspace/market/`.

**State persistence**: Each `code/execute` call pickles the current globals to a temp file, spawns a subprocess that loads the globals and runs user code, then pickles results back. The parent process merges new/changed variables. This gives real SIGKILL timeout enforcement while preserving state.

**Security**: Agent ID validated against strict allowlist (letters, digits, `-`, `_`, `.`). One container per agent - agents are isolated from each other.

**Image build**: `make sandbox-build` builds `sandbox-python:local`. `make sandbox-publish` pushes multi-platform image to GHCR.

## `code/execute`

Run Python code in the agent's persistent sandbox. Variables from previous calls are available. Provide **either** `code` (inline) **or** `file` (path to a `.py` in the workspace) - they are mutually exclusive.

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| `code` | string | one of | Inline Python code to execute |
| `file` | string | one of | Path to a `.py` script in the agent workspace. Accepts either sandbox-absolute (`/workspace/scripts/foo.py`) or agent-relative (`scripts/foo.py`). Trading-server reads the file from disk and streams its contents to the sandbox REPL as if it were inline `code`. |
| `timeout` | int | no | Seconds, default 30, max 120 |

**Inline vs file**: `code=` is the simple path for short snippets. `file=` is the pattern when the agent authored a longer script via Files MCP or Claude's Write tool and wants to run it without copying the full source into the LLM response. Only `.py` scripts are allowed for `file=`; to load CSVs or other assets use `code=` with `pd.read_csv(saved_to)`.

**Result (success):**
```json
{"stdout": "RSI: 72.3\nSignal: overbought", "stderr": "", "exit_code": 0, "duration_ms": 1250}
```

**Result (timeout):**
```json
{"stdout": "", "stderr": "TimeoutError: execution exceeded 2s and was killed", "exit_code": 124, "duration_ms": 2050}
```

**Result (Python error):**
```json
{"stdout": "", "stderr": "Traceback (most recent call last):\n  ...\nNameError: name 'ta' is not defined", "exit_code": 1, "duration_ms": 45}
```

**Typical usage** - read a CSV that Market Data MCP wrote, run analysis, print result:
```python
import pandas as pd
df = pd.read_csv('/workspace/market/ETH-USDC_1h.csv')
df['rsi'] = df['close'].rolling(14).apply(lambda x: ...)
print(f"RSI: {df['rsi'].iloc[-1]:.1f}")
```

State is preserved - import pandas once, reuse in later calls.

**Installing packages:**
```python
import subprocess
subprocess.run(['pip', 'install', 'httpx'], capture_output=True, check=True)
import httpx
```

## Integration Tests

Code MCP has integration tests in `golang/internal/mcps/codesandbox/service_integration_test.go` (build tag `integration`). Tests require Docker + `sandbox-python:local` image.

```bash
make sandbox-build              # build image first
make golang-test-integration    # run integration tests
```

Tests cover: simple output, state persistence across calls, agent isolation, timeout enforcement, syntax errors, pandas available, pip install + import, concurrent calls to same agent.
