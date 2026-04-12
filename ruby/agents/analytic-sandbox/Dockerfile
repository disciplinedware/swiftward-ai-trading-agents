# MCP Server — Go binary for Docker-in-Docker deployment
#
# Builds: MCP Server (stateful, creates sandbox containers)
#
# Requirements:
#   - Docker socket mounted at /var/run/docker.sock
#   - analytic-sandbox:latest image available (build with: make docker-build)
#
# Usage:
#   docker build -t mcp-server:latest .
#   docker run -v /var/run/docker.sock:/var/run/docker.sock -v ./data:/data mcp-server:latest

FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o bin/mcp-server ./cmd/server

# ---

FROM alpine:3.21

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /build/bin/mcp-server ./mcp-server

ENV MCP_SANDBOX_IMAGE=analytic-sandbox:latest

EXPOSE 9091

CMD ["./mcp-server"]