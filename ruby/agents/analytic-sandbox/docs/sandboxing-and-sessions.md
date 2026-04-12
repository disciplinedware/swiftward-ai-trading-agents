# Architecture: Containerized Sandboxing & Session Management

This document describes the current architecture of the Analytic Sandbox's containerized sandboxing and session management system.

**CONSTRAINT: This MCP Server is supported on LINUX ONLY due to dependencies on specific Docker bind-mount behaviors and user/group mapping features.**

## 1. High-Level Architecture

The system uses a **Container-Per-Session** model where the container's working directory is directly mapped to a specific host directory.

### Request Flow
`Request -> Session Manager -> Docker Client -> Container`

1. **Trigger**: A new `initialize` request (without `Mcp-Session-Id`) creates a session.
2. **Workspace Creation**: A dedicated directory `data/containers/<SessionID>` is created on the host.
3. **Containerization**: A Docker container is spawned using the configured `MCP_SANDBOX_IMAGE`.
    * **Workspace Mount**: `data/containers/<SessionID>` is mounted to `/app` (Read-Write).
    * **User Mapping**: The container runs as the host user's UID/GID to ensure file ownership compatibility.
    * **Resource Limits**: Configurable via HTTP headers (CPU, RAM, disk, timeout).
4. **Execution**: Tools (`shell`, `write_file`, `edit_file`) run inside or against the container. Execution is serialized per session.
5. **Output**: Results written to `/app` appear immediately in `data/containers/<SessionID>` for the host to read.

---

## 2. Core Components

### A. `internal/sandbox` (Docker Manager)
Wraps `github.com/docker/docker/client`.

**Responsibilities:**
* **Lifecycle**: Start/Stop containers with bind mounts, user mapping, and resource limits.
* **Cleanup**: Remove containers on shutdown or error.
* **Execution**: Run commands via `Exec` API with timeouts and output capture limits.
* **State**: Track container status.

### B. `internal/session`
The `Session` struct represents the active workspace and container.

```go
type Session struct {
    ID              string
    WorkdirUUID     string
    ContainerID     string
    HostWorkDir     string
    AllowNetwork    bool
    MCP             *server.MCPServer
    Handler         http.Handler
    ExecMutex       sync.Mutex
    CreatedAt       time.Time
    LastActivityAt  time.Time
    Stopped         bool
    Manager         *Manager
    MemoryMB        int
    CpuCount        int
    DiskMB          int
    TimeoutSeconds  int
}
```

### C. `cmd/server` (HTTP Bridge)
Parses `X-Sandbox-*` headers from the initialize request to configure session parameters, then delegates to `internal/session` for container lifecycle.

### D. `internal/mcp_handlers` (Tool Handlers)
Implements all MCP tools (`list_files`, `read_file`, `write_file`, `edit_file`, `grep_file`, `get_file_info`, `shell`).

---

## 3. Session Configuration (HTTP Headers)

Headers are read only on new session creation (no `Mcp-Session-Id`). On resume, parameters are restored from session metadata.

| Header | Default | Max |
|--------|---------|-----|
| `X-Sandbox-Allow-Network` | `false` | — |
| `X-Sandbox-Workdir-UUID` | (new UUID) | — |
| `X-Sandbox-Memory-MB` | `512` | `8192` |
| `X-Sandbox-Cpu-Count` | `4` | `16` |
| `X-Sandbox-Disk-MB` | `1024` | `51200` |
| `X-Sandbox-Timeout-Seconds` | `120` | `600` |

---

## 4. Tool Implementation Notes

### A. File Tools (Host-Side Execution)
Tools that read/write workspace files (`read_file`, `write_file`, `edit_file`, `list_files`, `grep_file`, `get_file_info`) operate on the **host** path (`Session.HostWorkDir`).

* **Memory Efficiency**: `bufio.Reader` and chunked processing (`256KB` chunks with `4KB` overlap for `grep_file`) handle multi-gigabyte files without crashing the server.
* **Width Limits**: Strict per-line `width_limit` (default `1000`) and the `... (skipped N bytes)` notation prevent token bloat in LLM responses.
* **MIME Detection**: Uses host-native `file --mime-type -b` for fast type identification.
* **Intelligent Truncation**: "Longest line first" truncation strategy for text headers to stay within `2048` character limits while preserving context.

### B. Shell Execution (Container-Side Execution)
The `shell` tool executes commands inside the **Docker Container**.

* **Serialized Execution**: `Session.ExecMutex` ensures only one command runs per session at a time.
* **Timeout**: Command timeout is configurable per session via `X-Sandbox-Timeout-Seconds` header (default: 120s, max: 600s).
* **Output Capture**: STDOUT/STDERR are captured with `limitWriter` (8KB in-memory, full output saved to `.outputs/` files on disk if truncated).

### C. Syntax Validation
`write_file` and `edit_file` validate syntax for `.py`, `.go`, and `.rb` files before saving changes, by running the respective compiler/interpreter check inside the container.

---

## 5. Workflow: Shell Command Execution

1. **User Request**: `shell` with `command="duckdb -c 'SELECT COUNT(*) FROM data'"`.
2. **Lock**: Session acquires `ExecMutex`.
3. **Execution**: Host triggers Docker Exec: `timeout -s KILL {timeout}s sh -c "{command}"` inside container.
4. **Monitoring**: Host waits for completion or timeout. Captures stderr, exit code, and streams output.
5. **Unlock**: Session releases lock.
6. **Response**: Returns exit code, execution time, STDOUT, and STDERR.

---

## 6. Lifecycle & Cleanup

* **Startup Cleanup**: On server start, remove existing containers with label `managed-by=mcp-server`.
* **Shutdown Cleanup**: Graceful shutdown handler stops/removes all active containers.
* **Inactivity Monitor**: Containers stopped after configurable timeout (default 30 min). Auto-restarted on next request.
* **Metadata Persistence**: Session parameters (CPU, RAM, disk, timeout, network) are saved to `data/sessions/<id>.json` and restored on resume.

---

## 7. Security & Safety

* **Read-Only Root FS**: Container root filesystem is immutable (`ReadonlyRootfs: true`).
* **Restricted Write Area**: Scripts can only write to `/app` (isolated host dir) or `/tmp` (RAM tmpfs, 64MB).
* **Network Isolation**: Blocked by default. Toggleable via `X-Sandbox-Allow-Network`.
* **Process Isolation**: `Init: true` prevents zombie processes.
* **Capability Dropping**: `CapDrop: ["ALL"]` + `no-new-privileges`.
* **Resource Limits**: Configurable per session, with server-side maximums enforced.
* **Timeouts**: Configurable per session command timeout (default 120s, max 600s).