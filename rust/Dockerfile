# Multi-stage build: Rust builder -> Alpine runtime
# Mirrors the Go Dockerfile pattern

FROM rust:1.85-alpine AS builder

RUN apk add --no-cache musl-dev pkgconfig openssl-dev openssl-libs-static

WORKDIR /app

# Cache dependencies - copy manifests first
COPY Cargo.toml Cargo.lock* ./

# Create dummy source to build deps
RUN mkdir -p src/bin && \
    echo 'fn main() {}' > src/bin/server.rs && \
    echo 'pub mod agents; pub mod config; pub mod mcp; pub mod platform;' > src/lib.rs && \
    mkdir -p src/agents/simple_llm src/config src/mcp src/platform && \
    touch src/agents/mod.rs src/agents/simple_llm/mod.rs src/config/mod.rs src/mcp/mod.rs src/platform/mod.rs && \
    touch src/agents/simple_llm/service.rs src/agents/simple_llm/tools.rs src/mcp/client.rs src/mcp/types.rs

# Build deps only (cached layer), then remove project artifacts so real source rebuilds
RUN cargo build --release 2>/dev/null || true && \
    rm -rf target/release/.fingerprint/ai-trading-agents-* && \
    rm -f target/release/deps/ai_trading_agents* \
          target/release/deps/server-* \
          target/release/server

# Copy real source and build
COPY src/ src/
RUN cargo build --release

# Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget

COPY --from=builder /app/target/release/server /usr/local/bin/server

ENTRYPOINT ["/usr/local/bin/server"]
