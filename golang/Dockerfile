# AI Trading Agents Platform — Go binary
# Builds: Trading MCP + Risk MCP + Files MCP + Code MCP + Random Agent (single binary, role-based)
#
# Usage:
#   docker build -t ai-trading-agents ./golang
#   docker compose up platform

FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o bin/server ./cmd/server

# ---

FROM alpine:3.21

RUN apk --no-cache add ca-certificates wget docker-cli

WORKDIR /app

COPY --from=builder /build/bin/server ./server

EXPOSE 8091

CMD ["./server"]
