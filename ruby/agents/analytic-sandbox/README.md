# Analytic Sandbox MCP Server

A high-performance, secure, and isolated environment for LLMs (Claude, ChatGPT, etc.) to perform complex data analysis. This multi-session, stateful Model Context Protocol (MCP) server allows AI agents to execute code, run SQL queries via DuckDB, and process large datasets using a professional data stack.

## Features
- **Professional Data Stack**: Pre-installed tools including DuckDB, Polars, Pandas, XSV, ripgrep, and more.
- **Context Bloat Protection**: Surgical file tools (`read_file`, `grep_file`) with `offset` and `count` to handle GB-sized files without overwhelming LLM context.
- **Stateful & Persistent Sessions**: Containers maintain state (variables, files, installed packages) across multiple turns and server restarts.
- **Hibernation System**: Inactive containers are paused and automatically resumed when needed, saving resources without losing progress.
- **Transparent Data Architecture**: Session files are mapped directly to the host (`data/containers/`), allowing easy human-agent collaboration.
- **On-the-fly Network Control**: Toggle internet access for sandboxes to ensure data privacy during sensitive analysis.
- **Parallel Scale & Experiment Integrity**: Run 100+ concurrent agents with zero cross-talk. Each session has its own UUID-based workspace, CPU/RAM limits, and I/O throttling to prevent "noisy neighbor" issues.
- **Sandboxed Execution**: Run Python, Ruby, and Go scripts in Docker containers with configurable resource limits.
- **Configurable Resources**: Memory, CPU, disk quota, and command timeout are configurable per session via HTTP headers.
- **Syntax Integrity Guard**: Automated pre-save validation for Python, Go, and Ruby to prevent workspace corruption.
- **Token-Based Authorization**: Optional Bearer token authentication via `.env` file.
- **Request/Response Logging**: Full payload diagnostics for debugging.
- **Transport**: Supports modern Streamable HTTP transport (SSE for events + HTTP for requests).
- **Docker-in-Docker**: Can run inside Docker with access to host Docker daemon.
- **Auto-cleanup**: Stops inactive containers after 30 minutes (configurable).

## Why Analytic Sandbox?

| Feature | Analytic Sandbox | Basic Implementations | Infrastructure-only Sandboxes |
|---------|------------------|-----------------------|----------------------|
| **Persistence** | ✅ Long-term state | ❌ One-off execution | ✅ State in paid tiers |
| **High-level Tools**| ✅ read_file, grep_file, get_file_info, Syntax Guard | ❌ None (only raw exec) | ❌ Raw CLI/Python only |
| **Token Usage** | ✅ Surgical (offset/count) | ❌ Reads whole files | ⚠️ Agent must write scripts |
| **Privacy** | ✅ 100% Local / Air-gap | ❌ Varies | ❌ Third-party cloud |
| **Debuggability**| ✅ Host-mapped files | ❌ Internal only | ❌ Cloud dashboard only |

## MCP Tools

| Tool | Description |
|------|-------------|
| `list_files` | Lists files and directories in the workspace |
| `read_file` | Reads lines from a file with pagination, hexdump mode for binary files |
| `write_file` | Creates or overwrites a file (with syntax validation for .py, .go, .rb) |
| `edit_file` | Edits a file (modes: block, line, string, regex) with syntax validation |
| `grep_file` | Searches for a regex pattern in a file with context lines |
| `get_file_info` | Returns file metadata: size, MIME type, line count, head/tail/random preview |
| `shell` | Executes a shell command inside the sandbox container |

## Session Configuration

When creating a new session (no `Mcp-Session-Id` header), you can configure resources via HTTP headers. If a header is omitted, the default value is used.

| Header | Description | Default | Max |
|--------|-------------|---------|-----|
| `X-Sandbox-Allow-Network` | Enable internet access (`true`/`1`) | `false` | — |
| `X-Sandbox-Workdir-UUID` | Resume an existing workspace by UUID | (new UUID) | — |
| `X-Sandbox-Memory-MB` | RAM limit in MB | `512` | `8192` |
| `X-Sandbox-Cpu-Count` | CPU cores | `4` | `16` |
| `X-Sandbox-Disk-MB` | Disk quota in MB | `1024` | `51200` |
| `X-Sandbox-Timeout-Seconds` | Command execution timeout in seconds | `120` | `600` |

All defaults are applied automatically — omit any header to use the default. Values above the maximum are clamped server-side.

### Example: Creating a High-Resource Session

```bash
curl -X POST http://localhost:9091/ \
  -H "Content-Type: application/json" \
  -H "X-Sandbox-Allow-Network: true" \
  -H "X-Sandbox-Memory-MB: 4096" \
  -H "X-Sandbox-Cpu-Count: 8" \
  -H "X-Sandbox-Disk-MB: 8192" \
  -H "X-Sandbox-Timeout-Seconds: 300" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{...}}'
```

## Authorization

The server supports optional Bearer token authentication following the MCP standard.

### Setup
1. Copy `.env.example` to `.env`:
   ```bash
   cp .env.example .env
   ```

2. Set your token in `.env`:
   ```bash
   MCP_AUTH_TOKEN=your-secret-token-here
   ```

3. All HTTP requests must include the token:
   ```bash
   curl -X POST http://localhost:9091/ \
     -H "Authorization: Bearer your-secret-token-here" \
     -H "Content-Type: application/json" \
     -d '{...}'
   ```

### Backward Compatibility
If `MCP_AUTH_TOKEN` is not set, the server runs without authentication (default behavior).

## Directory Structure

The server manages its workspace through the `data/` directory:

- `data/sessions/`: Automatically managed session metadata and history (JSON).
- `data/containers/`: Workspace directories for active Docker containers, mapped to host for easy inspection.

## Requirements
- Go 1.22+
- Docker

## Installation & Build
```bash
make build
```

## Usage
### Streamable HTTP (Recommended)
```bash
make run
```
Connect using Streamable HTTP: `http://localhost:9091/` (pass `Mcp-Session-Id` for stateful interactions).

### Standard MCP Client
Connect using the Streamable HTTP transport: `http://localhost:9091/mcp`

### Development & Debugging
- `make test`: Run all unit and integration tests.
- `make status`: View active sessions and resource usage.

## Quick Integration

### Claude Desktop
Add the following to your `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "analytic-sandbox": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "-v", "/var/run/docker.sock:/var/run/docker.sock",
        "-v", "./data:/app/data",
        "analytic-server:latest"
      ]
    }
  }
}
```

### Cursor / Windsurf
In **Settings -> Features -> MCP**, add a new server with the following command:
```bash
docker run -i --rm -v /var/run/docker.sock:/var/run/docker.sock -v ./data:/app/data analytic-server:latest
```

## Security & Sandboxing

The Analytic Sandbox is built with security as a top priority:
- **Session Isolation**: Each agent session runs in a dedicated Docker container with configurable CPU, RAM, and disk limits.
- **Network Control**: On-the-fly network toggle via `X-Sandbox-Allow-Network` header.
- **Resource Constraints**: Built-in limits on STDOUT/STDERR prevent "output-bombing" and resource exhaustion.
- **Host Protection**: Only the `data/` directory is mapped to the sandbox, ensuring the rest of your system remains unreachable.

## Health & Status Endpoints

### `/health`
Basic health check (no auth required):
```bash
curl http://localhost:9091/health
# {"status":"ok"}
```

### `/status`
Detailed server status (requires MCP_AUTH_TOKEN):
```bash
curl -H "Authorization: Bearer your-token" http://localhost:9091/status
# {
#   "sessions": [...],
#   "active_count": 2,
#   "stopped_count": 1,
#   "total_sessions": 3
# }
```

## Docker-in-Docker Deployment

The MCP server can run inside Docker while creating sandbox containers on the host Docker daemon.

### Prerequisites
1. Build the images:
   ```bash
   make docker-build-all
   ```

2. (Optional) Build manually:
   ```bash
   docker build -f Dockerfile.sandbox -t analytic-sandbox:latest .
   docker build -t analytic-server:latest .
   ```

### Running with Docker Compose

Copy `docker-compose.yml.sample` and configure:

```yaml
services:
  analytic-server:
    image: analytic-server:latest
    ports:
      - "9091:9091"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/app/data
    environment:
      - MCP_SANDBOX_IMAGE=analytic-sandbox:latest
      - MCP_AUTH_TOKEN=${MCP_AUTH_TOKEN:-}
```

**Important:**
- `/var/run/docker.sock` must be mounted for container creation
- `analytic-sandbox:latest` image must exist on the host
- `./data` directory persists session data

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_SANDBOX_IMAGE` | Docker image for sandbox containers | `analytic-sandbox:latest` |
| `MCP_AUTH_TOKEN` | Bearer token for authorization | (none) |
| `HOST_DATA_DIR` | Absolute path to data directory on the **host** machine. Required when running the server inside Docker to correctly map volumes to sandbox containers. | (none) |

### How It Works

1. MCP server runs inside a container
2. Docker socket mounted from host
3. Server creates sandbox containers on **host** Docker daemon
4. Sandbox containers run with the same privileges as other host containers
5. Data directory persists across restarts

### Inactivity Cleanup

Containers are automatically stopped after 30 minutes of inactivity (configurable via `-inactivity-timeout` flag). On next request, the container is restarted automatically. On server shutdown, all containers are cleaned up.