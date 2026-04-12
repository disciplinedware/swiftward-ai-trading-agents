# Stage 1: Build the claude-agent Go binary
FROM golang:1.25-alpine AS build

WORKDIR /build
COPY golang/ .

RUN go build -o /build/claude-agent -ldflags "-s -w" ./cmd/claude-agent

# Stage 2: Runtime — node:24-alpine with Claude Code + Python + our binary
FROM node:24-alpine

ENV TERM=xterm-color \
    APP_USER=app \
    APP_UID=1001

# Python 3 + pip (agent analysis scripts), git/bash/curl/jq/ripgrep (Claude's native tools)
RUN apk add --no-cache \
    python3 py3-pip \
    git bash curl jq ca-certificates \
    libgcc libstdc++ ripgrep

# Use system ripgrep (already installed above)
ENV USE_BUILTIN_RIPGREP=0

# Install Claude Code — pinned version, upgrade deliberately
# Version 2.1.87: tested with MCP streamable HTTP transport and --print mode
RUN npm install -g @anthropic-ai/claude-code@2.1.87 && \
    command -v claude >/dev/null || { echo "error: claude not found after install"; exit 1; }

# Install Python analysis packages — prebuilt Alpine APKs (seconds, not minutes)
RUN apk add --no-cache py3-pandas py3-numpy py3-scipy

# Copy Go binary from build stage
COPY --from=build /build/claude-agent /usr/local/bin/claude-agent
RUN chmod +x /usr/local/bin/claude-agent

# Non-root user
RUN adduser -s /bin/bash -D -u ${APP_UID} ${APP_USER}

# Claude reads CLAUDE_CONFIG_DIR for credentials, settings.json, and auto-memory
ENV CLAUDE_CONFIG_DIR=/home/app/.claude
RUN mkdir -p /home/app/.claude && chown ${APP_UID}:${APP_UID} /home/app/.claude

COPY docker/claude-agent/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

USER ${APP_USER}
WORKDIR /workspace

ENTRYPOINT ["/entrypoint.sh"]
# claude-agent binary is the main process — it orchestrates claude as a child process
CMD ["/usr/local/bin/claude-agent"]
