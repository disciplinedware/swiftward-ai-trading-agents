# Solid Loop Trading Agent

Trading agent orchestrator with MCP-based tools and an integrated data sandbox.

## Data Synchronization between Orchestrator and Sandbox

To ensure the Trading Agent can successfully download market data (via the Trading MCP) and analyze it (via the Analytic Sandbox MCP), both services must share the same physical directory for their working data.

The system uses a unique `workdir_uuid` (persisted in the loop's state) to create a shared folder: `storage/external/data/{uuid}`.

### Method 1: Local Development (Symlink)

If you are running the Rails app and the Analytic Sandbox on the same host machine, the easiest way is to symlink the Rails storage directory to the sandbox container storage.

Run this command from the root of the `solid_loop_trading` project:

```bash
# Make sure the target directory exists in analytic-sandbox
mkdir -p ../analytic-sandbox/data/containers

# (Optional) Move any existing data
mv storage/external/data/* ../analytic-sandbox/data/containers/ 2>/dev/null || true

# Remove the local folder and create the symlink
rm -rf storage/external/data
ln -s ../analytic-sandbox/data/containers storage/external/data
```

Now, any files written by the Rails app (via `McpTools::GetCandles`) will be instantly visible inside the sandbox Docker containers at the `/app` path.

### Method 2: Docker / Production (Shared Bind Mount)

If both the Orchestrator and the Sandbox are running inside Docker, you should use a shared volume or a bind mount to the same host directory.

In your `docker-compose.yml`:

```yaml
services:
  orchestrator:
    # ...
    volumes:
      - /opt/solid_loop/data:/app/storage/external/data

  analytic_sandbox:
    # ...
    volumes:
      - /opt/solid_loop/data:/app/data/containers
```

---

## Configuration

Set the following environment variables in your `.env` file:

* `TRADING_MCP_URL`: URL of the trading MCP server (default: `http://localhost:3000/mcp`)
* `MCP_SERVER`: URL of the analytic sandbox MCP server (default: `http://localhost:8080`)
* `MCP_API_TOKEN`: Authorization token for the sandbox server

## Setup

1. `bundle install`
2. `bin/rails db:setup` (this will also load trading task prompts from `docs/prompts/*.md`)
3. `bin/rails s`
