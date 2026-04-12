#!/bin/sh
# entrypoint.sh - runs at container startup before the main command
#
# 1. Copies Claude credentials from the read-only host mount (/mnt/claude)
#    into the writable per-agent CLAUDE_HOME (/home/app/.claude).
#    Each agent has its own volume so their auto-memory stays isolated.
#    Credentials are copied on every start (like ralphex) so host re-login
#    is picked up automatically.
#
# 2. Writes settings.json once (skip if present — Claude may add entries during sessions).
#
# 3. Writes .mcp.json from env vars on every start so URL changes in compose take effect.
#
# Compose wires:
#   ${HOME}/.claude             → /mnt/claude            (read-only, host credentials)
#   data/claude-home/<agent>/   → /home/app/.claude      (writable, per-agent memory)
#   data/workspace/<agent>/     → /workspace             (writable, agent files)
#   prompts/claude-agent/       → /workspace/.claude     (read-only, CLAUDE.md + agents)

set -e

CLAUDE_HOME="${CLAUDE_AGENT__CLAUDE_HOME:-/home/app/.claude}"
mkdir -p "${CLAUDE_HOME}"

# ── Credentials ───────────────────────────────────────────────────────────────
# Three sources, in priority order (same as ralphex init-docker.sh):
#   1. /mnt/claude/.credentials.json — host ~/.claude mounted read-only
#   2. /mnt/claude-credentials.json — extracted from macOS Keychain by wrapper script
#   3. ANTHROPIC_API_KEY env var — fallback for CI / non-Mac hosts
if [ -f "/mnt/claude/.credentials.json" ]; then
    cp "/mnt/claude/.credentials.json" "${CLAUDE_HOME}/.credentials.json"
    chmod 600 "${CLAUDE_HOME}/.credentials.json"
    echo "[entrypoint] credentials copied from /mnt/claude"
elif [ -f "/mnt/claude-credentials.json" ]; then
    cp "/mnt/claude-credentials.json" "${CLAUDE_HOME}/.credentials.json"
    chmod 600 "${CLAUDE_HOME}/.credentials.json"
    echo "[entrypoint] credentials copied from Keychain extract"
elif [ -z "${ANTHROPIC_API_KEY:-}" ]; then
    echo "[entrypoint] warning: no credentials found"
    echo "[entrypoint] run 'claude' on your host to log in, or set ANTHROPIC_API_KEY"
fi

# ── settings.json ─────────────────────────────────────────────────────────────
# Written only if missing — Claude Code may add trust entries during sessions
# and we don't want to wipe them on restart.
# enableAllProjectMcpServers: suppresses interactive approval in --print mode.
if [ ! -f "${CLAUDE_HOME}/settings.json" ]; then
    cat > "${CLAUDE_HOME}/settings.json" << 'EOF'
{
  "enableAllProjectMcpServers": true
}
EOF
    echo "[entrypoint] settings.json created"
fi

# ── .mcp.json ─────────────────────────────────────────────────────────────────
# Written on every start so compose env var changes (e.g. different URLs for
# alpha vs beta) take effect immediately.
# Claude Code picks this up from the workspace root as project-scope MCP config.
# Read from CLAUDE_AGENT__* vars (Go runtime reads these via koanf).
# Entrypoint writes them into .mcp.json for Claude Code.
TRADING_MCP_URL="${CLAUDE_AGENT__TRADING_MCP_URL:-http://swiftward-server:8095/mcp/trading}"
MARKET_MCP_URL="${CLAUDE_AGENT__MARKET_MCP_URL:-http://swiftward-server:8095/mcp/market}"
NEWS_MCP_URL="${CLAUDE_AGENT__NEWS_MCP_URL:-http://swiftward-server:8095/mcp/news}"
POLYMARKET_MCP_URL="${CLAUDE_AGENT__POLYMARKET_MCP_URL:-http://swiftward-server:8095/mcp/polymarket}"
AGENT_ID="${CLAUDE_AGENT__AGENT_ID:-agent-claude-alpha}"

# Fail with a clear message if /workspace is not writable (e.g. Linux host with
# root-owned bind mount dir). On macOS Docker Desktop this never happens.
if ! touch "/workspace/.mcp.json" 2>/dev/null; then
    echo "[entrypoint] ERROR: cannot write to /workspace"
    echo "[entrypoint] On Linux: ensure data/workspace/<agent>/ is owned by UID 1001"
    exit 1
fi

cat > "/workspace/.mcp.json" << EOF
{
  "mcpServers": {
    "trading": {
      "type": "http",
      "url": "${TRADING_MCP_URL}",
      "headers": {
        "X-Agent-ID": "${AGENT_ID}"
      }
    },
    "market": {
      "type": "http",
      "url": "${MARKET_MCP_URL}",
      "headers": {
        "X-Agent-ID": "${AGENT_ID}"
      }
    },
    "news": {
      "type": "http",
      "url": "${NEWS_MCP_URL}",
      "headers": {
        "X-Agent-ID": "${AGENT_ID}"
      }
    },
    "polymarket": {
      "type": "http",
      "url": "${POLYMARKET_MCP_URL}",
      "headers": {
        "X-Agent-ID": "${AGENT_ID}"
      }
    }
  }
}
EOF

echo "[entrypoint] .mcp.json written (trading=${TRADING_MCP_URL} polymarket=${POLYMARKET_MCP_URL})"

exec "$@"
