# Installation & Setup Guide

This guide will walk you through setting up **Analytic Sandbox** on your local machine or server.

## Prerequisites

- **Go 1.22+**: For building the MCP server.
- **Docker**: For running isolated sandbox environments.
- **Access to `/var/run/docker.sock`**: The server needs to manage containers on your behalf.

---

## 1. Installation

### Clone the repository
```bash
git clone https://github.com/your-repo/analytic-sandbox.git
cd analytic-sandbox
```

### Build the binaries
```bash
make build
```

### Build the Docker images
You need to build the sandbox environment image and the server image:
```bash
make docker-build-all
```

---

## 2. Configuration

The server uses environment variables and HTTP headers for configuration.

### Environment Variables
| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_AUTH_TOKEN` | Bearer token for authorization. | (none) |
| `MCP_SANDBOX_IMAGE` | Docker image name for sandboxes. | `analytic-sandbox:latest` |
| `ADDR` | Port to listen on (e.g., `:9091`). | `:9091` (Streamable HTTP) |
| `HOST_DATA_DIR` | Absolute path to data directory on the **host**. Required when running inside Docker. | (none) |

### Session Headers
Configuration per session is done via HTTP headers on the `initialize` request:

| Header | Description | Default | Max |
|--------|-------------|---------|-----|
| `X-Sandbox-Allow-Network` | Enable internet access | `false` | — |
| `X-Sandbox-Workdir-UUID` | Resume existing workspace | (new UUID) | — |
| `X-Sandbox-Memory-MB` | RAM limit (MB) | `512` | `8192` |
| `X-Sandbox-Cpu-Count` | CPU cores | `4` | `16` |
| `X-Sandbox-Disk-MB` | Disk quota (MB) | `1024` | `51200` |
| `X-Sandbox-Timeout-Seconds` | Command timeout (s) | `120` | `600` |

### Setting up Authorization
1. Copy `.env.example` to `.env`.
2. Generate a secure token and paste it into `MCP_AUTH_TOKEN`.
3. Restart the server.

---

## 3. Data Directory Organization

The server manages its workspace through the `data/` directory:

- `data/sessions/`: Automatically managed session metadata and history (JSON). Includes session parameters (CPU, RAM, disk, timeout, network) for persistence across restarts.
- `data/containers/`: Workspace directories for active Docker containers. Each session gets a UUID-based subdirectory mapped to the container's `/app`.

---

## 4. Troubleshooting

### Docker Permissions
If you get "permission denied" when accessing the Docker socket:
- Linux: Add your user to the `docker` group: `sudo usermod -aG docker $USER`.
- Docker Desktop: Ensure "Allow the default Docker socket to be used" is checked in settings.

### Slow Sandbox Startup
The first time you run a command, it might take a few seconds to initialize the Docker container. Subsequent calls in the same session will be near-instant.