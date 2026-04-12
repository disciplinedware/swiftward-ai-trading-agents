#!/bin/sh
# Extract Claude Code credentials from macOS Keychain to a temp file.
# Pattern borrowed from ralphex (ralphex-dk.sh).
#
# Claude Code stores auth in macOS Keychain under service "Claude Code-credentials".
# This script extracts it so Docker containers can use it.

set -e

CLAUDE_HOME="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
CREDS_FILE="$CLAUDE_HOME/.credentials.json"

# If credentials file already exists on disk, nothing to do
if [ -f "$CREDS_FILE" ]; then
    echo "$CREDS_FILE"
    exit 0
fi

# macOS only - extract from Keychain
if [ "$(uname)" != "Darwin" ]; then
    echo "error: no credentials file at $CREDS_FILE and not on macOS (cannot extract from Keychain)" >&2
    exit 1
fi

# Derive Keychain service name (same logic as ralphex)
RESOLVED_HOME="$(cd "$CLAUDE_HOME" 2>/dev/null && pwd -P || echo "$CLAUDE_HOME")"
DEFAULT_HOME="$(cd "$HOME/.claude" 2>/dev/null && pwd -P || echo "$HOME/.claude")"

if [ "$RESOLVED_HOME" = "$DEFAULT_HOME" ]; then
    SERVICE="Claude Code-credentials"
else
    HASH=$(printf '%s' "$RESOLVED_HOME" | shasum -a 256 | cut -c1-8)
    SERVICE="Claude Code-credentials-$HASH"
fi

# Try to read from Keychain
CREDS=$(security find-generic-password -s "$SERVICE" -w 2>/dev/null || true)

if [ -z "$CREDS" ]; then
    # Keychain might be locked - try unlocking
    echo "Unlocking macOS Keychain for Claude credentials..." >&2
    security unlock-keychain 2>/dev/null || true
    CREDS=$(security find-generic-password -s "$SERVICE" -w 2>/dev/null || true)
fi

if [ -z "$CREDS" ]; then
    echo "error: could not extract Claude credentials from Keychain (service: $SERVICE)" >&2
    echo "Run 'claude' on your host and log in first." >&2
    exit 1
fi

# Write to temp file
TMPFILE=$(mktemp)
echo "$CREDS" > "$TMPFILE"
chmod 600 "$TMPFILE"
echo "$TMPFILE"
