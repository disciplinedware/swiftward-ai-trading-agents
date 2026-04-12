.PHONY: help proto-sync swiftward-build-local swiftward-publish sandbox-build sandbox-publish analytic-sandbox-build up local-up down infra local-infra infra-golang infra-rust infra-python reset redis-flush build test lint clean agent-claude-build agent-claude-publish demo-ruby ruby-down python-down logs-down logs java-build java-test demo-java java-down

-include .env
export

export PATH := $(HOME)/.foundry/bin:$(PATH)

# Auto-set HOST_WORKSPACE_PATH so Code MCP can bind-mount the workspace into sandbox containers.
# Overridable via .env if the project lives on a different host path.
HOST_WORKSPACE_PATH ?= $(PWD)/data/workspace

SWIFTWARD_CORE := ../swiftward-core
REGISTRY := ghcr.io/disciplinedware/ai-trading-agents
TAG := latest

# ============================================================
# AI Trading Agents Platform
# ============================================================

check-env: ## Check that .env file exists
	@if [ ! -f .env ]; then \
		echo "\033[31mError: .env file not found.\033[0m"; \
		echo "Copy the example and configure it:"; \
		echo "  cp .env.example .env"; \
		echo "  \$$EDITOR .env"; \
		exit 1; \
	fi

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

# ============================================================
# Swiftward — build, publish, run
# ============================================================

REPO_URL := https://github.com/disciplinedware/swiftward-ai-trading-agents

proto-sync: ## Copy generated protobuf files from swiftward-core
	mkdir -p golang/internal/proto
	cp $(SWIFTWARD_CORE)/golang/internal/proto/common.pb.go golang/internal/proto/
	cp $(SWIFTWARD_CORE)/golang/internal/proto/ingestion.pb.go golang/internal/proto/
	cp $(SWIFTWARD_CORE)/golang/internal/proto/ingestion_grpc.pb.go golang/internal/proto/

swiftward-build-local: ## Build swiftward-server:local (single image: server + migrations + control UI)
	docker build \
		-t swiftward-server:local \
		-f $(SWIFTWARD_CORE)/golang/Dockerfile \
		--build-arg APP_NAME=server \
		--label org.opencontainers.image.source=$(REPO_URL) \
		$(SWIFTWARD_CORE)


swiftward-publish: ## Build Swiftward server image multi-platform + push to GHCR [Kostya only]
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--push \
		-t $(REGISTRY)/swiftward-server:$(TAG) \
		-f $(SWIFTWARD_CORE)/golang/Dockerfile \
		--build-arg APP_NAME=server \
		--label org.opencontainers.image.source=$(REPO_URL) \
		$(SWIFTWARD_CORE)
	@echo ""
	@echo "Published multi-platform (amd64+arm64) to $(REGISTRY)/swiftward-server:$(TAG)"
	@echo "Colleagues: make up (image pulls automatically)"

# ============================================================
# Full Stack (Docker Compose)
# ============================================================

# PROFILES: comma-separated list of agent profiles to start.
# EVERY agent is profile-gated, including random. Without PROFILES only infra starts.
# Profile names come from compose.yaml. Verify there before adding new ones.
# Available profiles (as of current compose.yaml):
#   random               - random trader demo agent
#   alpha / gamma        - Claude Code agents
#   delta                - Go LLM agent (needs OPENAI_API_KEY)
#   epsilon              - Rust LLM agent (needs OPENAI_API_KEY)
#   midas                - LIVE Kraken trader (use make live-up, not make up)
#   java                 - Java Fear & Greed agent
#   ruby                 - Ruby Agents Arena (backtesting + live)
#   python               - Python orchestrated agent
# Examples:
#   make up PROFILES=random              # random only
#   make up PROFILES=random,alpha        # random + Claude alpha
#   make up PROFILES=alpha               # Claude alpha only
# NOTE: `comma` MUST be declared BEFORE PROFILE_FLAGS. Make expands variables
# at use-site, but `$(comma)` inside the PROFILE_FLAGS definition is resolved
# when PROFILE_FLAGS is used, not when it is defined — so the order of lines
# in the file matters only for RHS-of-RHS references like the one below, where
# `$(subst $(comma),...)` needs `comma` to already carry the `,` value before
# the outer expansion runs. Swapping the order silently collapses multi-value
# PROFILES into a single --profile arg like `--profile random,alpha,python`,
# which docker compose interprets as one literal (non-matching) profile name
# and every profile-gated service stays down.
comma := ,
PROFILE_FLAGS := $(if $(PROFILES),$(foreach p,$(subst $(comma), ,$(PROFILES)),--profile $(p)),)

up: check-env ## Start stack. PROFILES=random,alpha|gamma|delta|epsilon|ruby|java|python (comma-separated)
	@if echo "$(PROFILES)" | grep -q "midas"; then echo "ERROR: Midas is live-only. Use: make live-up"; exit 1; fi
	@mkdir -p data/workspace
	@if echo "$(PROFILES)" | grep -q "alpha\|gamma"; then \
		mkdir -p data/claude-home/agent-alpha-claude data/workspace/agent-alpha-claude; \
		mkdir -p data/claude-home/agent-gamma-claude data/workspace/agent-gamma-claude; \
		CREDS=$$(./scripts/extract-claude-credentials.sh) && \
		CLAUDE_CREDS_FILE=$$CREDS \
		docker compose $(PROFILE_FLAGS) up --build -d; \
		EXIT=$$?; [ -n "$$CREDS" ] && [ "$$CREDS" != "$$HOME/.claude/.credentials.json" ] && rm -f "$$CREDS"; exit $$EXIT; \
	else \
		docker compose $(PROFILE_FLAGS) up --build -d; \
	fi

local-up: check-env swiftward-build-local ## Same as up but also builds swiftward from source. Same PROFILES syntax as up.
	@if echo "$(PROFILES)" | grep -q "midas"; then echo "ERROR: Midas is live-only. Use: make live-up"; exit 1; fi
	@mkdir -p data/workspace
	@if echo "$(PROFILES)" | grep -q "alpha\|gamma"; then \
		mkdir -p data/claude-home/agent-alpha-claude data/workspace/agent-alpha-claude; \
		mkdir -p data/claude-home/agent-gamma-claude data/workspace/agent-gamma-claude; \
		CREDS=$$(./scripts/extract-claude-credentials.sh) && \
		SWIFTWARD_SERVER_IMAGE=swiftward-server:local \
		CLAUDE_CREDS_FILE=$$CREDS \
		docker compose $(PROFILE_FLAGS) up --build -d; \
		EXIT=$$?; [ -n "$$CREDS" ] && [ "$$CREDS" != "$$HOME/.claude/.credentials.json" ] && rm -f "$$CREDS"; exit $$EXIT; \
	else \
		SWIFTWARD_SERVER_IMAGE=swiftward-server:local docker compose $(PROFILE_FLAGS) up --build -d; \
	fi

live-up: check-env ## Start LIVE Kraken trading (Midas agent only, REAL MONEY)
	@echo "=== LIVE TRADING - Swiftward Midas on real Kraken account ==="
	@mkdir -p data/claude-home/agent-midas-claude data/workspace/agent-midas-claude
	@CREDS=$$(./scripts/extract-claude-credentials.sh) && \
	TRADING__EXCHANGE__MODE=kraken_real \
	CLAUDE_CREDS_FILE=$$CREDS \
	docker compose --profile midas up --build -d; \
	EXIT=$$?; [ -n "$$CREDS" ] && [ "$$CREDS" != "$$HOME/.claude/.credentials.json" ] && rm -f "$$CREDS"; exit $$EXIT

down: ## Stop full stack (all known profiles)
	docker compose \
		--profile random \
		--profile alpha --profile gamma \
		--profile delta --profile epsilon \
		--profile java --profile ruby --profile python \
		--profile midas \
		down

logs-down: ## Stop observability stack (SigNoz), reclaims RAM/Disk
	docker compose stop signoz-clickhouse signoz-zookeeper signoz signoz-otel-collector
	docker compose rm -f signoz-clickhouse signoz-zookeeper signoz signoz-otel-collector

infra: check-env ## Start infra only (no agents)
	@mkdir -p data/workspace
	docker compose pull --ignore-buildable
	docker compose up --build -d

local-infra: swiftward-build-local ## Build locally + start infra only
	@mkdir -p data/workspace
	SWIFTWARD_SERVER_IMAGE=swiftward-server:local docker compose up --build -d

python-down: ## Stop Python trader services only
	docker compose stop py-agent py-price-feed-mcp py-news-mcp py-onchain-data-mcp py-fear-greed-mcp
	docker compose rm -f py-agent py-price-feed-mcp py-news-mcp py-onchain-data-mcp py-fear-greed-mcp

redis-flush: ## Flush all Redis keys (clears news/price/agent caches)
	docker compose exec redis redis-cli FLUSHALL

reset: ## Full reset — stop, destroy volumes + data, restart from scratch
	docker compose down -v
	rm -rf data/workspace
	@echo "Volumes removed. Run 'make up' to start fresh."

logs: ## Tail all service logs
	docker compose logs -f

# ============================================================
# Go — MCP servers, platform services
# ============================================================

trading-server-build: ## Build trading-server:local (Go binary + embedded dashboard UI)
	docker build \
		-t trading-server:local \
		-f Dockerfile.trading-server \
		--label org.opencontainers.image.source=$(REPO_URL) \
		.

trading-server-publish: ## Build + push trading-server image multi-platform to GHCR [Kostya only]
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--push \
		-t $(REGISTRY)/trading-server:$(TAG) \
		-f Dockerfile.trading-server \
		--label org.opencontainers.image.source=$(REPO_URL) \
		.
	@echo "Published multi-platform $(REGISTRY)/trading-server:$(TAG)"

sandbox-build: ## Build Python sandbox image :local (for Code MCP)
	docker build \
		-t sandbox-python:local \
		--label org.opencontainers.image.source=$(REPO_URL) \
		golang/internal/mcps/codesandbox

analytic-sandbox-build: ## Build analytic sandbox image :local (for Ruby agent Code MCP)
	docker build \
		-f ruby/agents/analytic-sandbox/Dockerfile.sandbox \
		-t analytic-sandbox:local \
		--label org.opencontainers.image.source=$(REPO_URL) \
		ruby/agents/analytic-sandbox

sandbox-publish: ## Build + push Python sandbox image multi-platform to GHCR [Kostya only]
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--push \
		-t $(REGISTRY)/sandbox-python:$(TAG) \
		--label org.opencontainers.image.source=$(REPO_URL) \
		golang/internal/mcps/codesandbox
	@echo "Published multi-platform $(REGISTRY)/sandbox-python:$(TAG)"

# ============================================================
# Claude Agent — Docker image + credentials setup
# ============================================================

agent-claude-build: ## Build claude-agent:local image (Go harness + Claude Code + Python)
	docker build \
		-t claude-agent:local \
		--label org.opencontainers.image.source=$(REPO_URL) \
		-f docker/claude-agent/Dockerfile \
		.

agent-claude-publish: ## Build + push claude-agent image multi-platform to GHCR [Kostya only]
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--push \
		-t $(REGISTRY)/claude-agent:$(TAG) \
		--label org.opencontainers.image.source=$(REPO_URL) \
		-f docker/claude-agent/Dockerfile \
		.
	@echo "Published multi-platform $(REGISTRY)/claude-agent:$(TAG)"


golang-build: proto-sync ## Build all Go binaries
	cd golang && go build -o bin/ ./cmd/...

golang-test: ## Run Go unit tests
	cd golang && go test -timeout 120s ./... -v

golang-test-integration: ## Run Go integration tests (requires Docker + sandbox image + Binance API)
	cd golang && go test -tags=integration ./... -v

golang-lint: ## Lint Go code
	cd golang && golangci-lint run ./...

golang-tidy: ## Tidy Go modules
	cd golang && go mod tidy

gen-wallets: ## Generate Ethereum wallets for ERC-8004 (.env setup, run once)
	cd golang && go run ./cmd/gen-wallets

erc8004-ipfs-upload: ## Upload agent JSON to IPFS. Usage: make erc8004-ipfs-upload AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make erc8004-ipfs-upload AGENT=DELTA_GO"; exit 1; fi
	@FILE="erc8004/agents/$$(echo $(AGENT) | tr '[:upper:]' '[:lower:]').json"; \
		if [ ! -f "$$FILE" ]; then echo "ERROR: $$FILE not found"; exit 1; fi; \
		set -a && . ./.env && set +a && \
		RESPONSE=$$(curl -s -X POST "https://api.pinata.cloud/pinning/pinFileToIPFS" \
			-H "Authorization: Bearer $$PINATA_JWT" \
			-F "file=@$$FILE" \
			-F "pinataMetadata={\"name\":\"$$(basename $$FILE)\"}") && \
		CID=$$(echo "$$RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['IpfsHash'])" 2>/dev/null) && \
		if [ -z "$$CID" ]; then echo "ERROR: Upload failed: $$RESPONSE"; exit 1; fi && \
		echo "" && \
		echo "=== IPFS UPLOAD SUCCESSFUL ===" && \
		echo "File: $$FILE" && \
		echo "" && \
		echo "Set in .env:" && \
		echo "AGENT_$(AGENT)_REGISTRATION_URI=ipfs://$$CID" && \
		echo "" && \
		echo "Verify: https://gateway.pinata.cloud/ipfs/$$CID"

register-agent-hackathon: ## Register agent on hackathon's AgentRegistry. Usage: make register-agent-hackathon AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make register-agent-hackathon AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) REGISTRY=hackathon go run ./cmd/erc8004-setup register

register-agent-standard: ## Register agent on standard ERC-8004 registry. Usage: make register-agent-standard AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make register-agent-standard AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) REGISTRY=standard go run ./cmd/erc8004-setup register

agent-intel: ## Full pipeline: sync blockchain + market data, calculate PnL, generate site
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/agent-intel all

agent-intel-sync: ## Download new blockchain events + market data (all phases)
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/agent-intel sync

agent-intel-sync-events: ## Sync only event logs (fast: trade/reputation/attestation events, no snapshots)
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/agent-intel sync-events

agent-intel-sync-state: ## Sync only snapshot state (batched view calls for scores/vault)
	@set -a && . ./.env && set +a && cd golang && AGENT_INTEL_FORCE_STATE=1 go run ./cmd/agent-intel sync-state

agent-intel-sync-market: ## Sync only Kraken market data (per-pair, skip-if-fresh)
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/agent-intel sync-market

agent-intel-calc: ## Run FIFO PnL calculation on downloaded data
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/agent-intel calculate

agent-intel-site: ## Generate static HTML site from computed data
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/agent-intel generate

spy-agents: ## Read all competitor agent data from hackathon contracts (read-only, no gas)
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/leaderboard-spy agents

spy-trades: ## Read all trade events from RiskRouter (read-only, no gas)
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/leaderboard-spy trades

spy-attestations: ## Read attestations for an agent. Usage: make spy-attestations ID=0
	@if [ -z "$(ID)" ]; then echo "ERROR: ID not set. Usage: make spy-attestations ID=0"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && go run ./cmd/leaderboard-spy attestations $(ID)

claim-vault: ## Claim hackathon vault allocation. Usage: make claim-vault AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make claim-vault AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) go run ./cmd/erc8004-setup claim-vault

erc8004-set-uri: ## Update agent registration URI on-chain. Usage: make erc8004-set-uri AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make erc8004-set-uri AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) go run ./cmd/erc8004-setup set-uri

erc8004-set-wallet: ## Link SwiftwardAgentWallet to agent identity. Usage: make erc8004-set-wallet AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make erc8004-set-wallet AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) go run ./cmd/erc8004-setup set-wallet

erc8004-validate: ## Smoke-test ERC-8004 validation flow. Usage: make erc8004-validate AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make erc8004-validate AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) go run ./cmd/erc8004-setup validate

erc8004-feedback: ## Post reputation feedback (6 metrics) to ERC-8004 ReputationRegistry. Usage: make erc8004-feedback AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make erc8004-feedback AGENT=DELTA_GO"; exit 1; fi
	@set -a && . ./.env && set +a && cd golang && AGENT=$(AGENT) go run ./cmd/erc8004-setup feedback

erc8004-booster: ## Boost reputation. Usage: make erc8004-booster FROM=DELTA_GO TO=RANDOM [SCORE=100]
	@if [ -z "$(FROM)" ]; then echo "ERROR: FROM not set. Usage: make erc8004-booster FROM=DELTA_GO TO=RANDOM"; exit 1; fi
	@if [ -z "$(TO)" ]; then echo "ERROR: TO not set. Usage: make erc8004-booster FROM=DELTA_GO TO=RANDOM"; exit 1; fi
	@set -a && . ./.env && set +a && \
		KEY=$$(grep -E "^AGENT_$(FROM)_PRIVATE_KEY=" .env | cut -d= -f2) && \
		if [ -z "$$KEY" ]; then echo "ERROR: AGENT_$(FROM)_PRIVATE_KEY not found in .env"; exit 1; fi && \
		TARGET_ID=$$(grep -E "AGENT_$(TO)_(ERC8004|HACKATHON|STANDARD)_AGENT_ID=" .env | grep -v '^#' | head -1 | cut -d= -f2) && \
		if [ -z "$$TARGET_ID" ]; then echo "ERROR: AGENT_$(TO)_*_AGENT_ID not found in .env"; exit 1; fi && \
		cd golang && BOOST_TARGETS="$$TARGET_ID" BOOST_RATER_KEYS="$$KEY" BOOST_SCORE="$(SCORE)" BOOST_COMMENT="$(COMMENT)" AGENT=_ go run ./cmd/erc8004-setup booster

# ============================================================
# Solidity — AgentWallet (EIP-1271) contract
# ============================================================

solidity-build: ## Compile Solidity contracts
	cd solidity && forge build

solidity-test: ## Run Foundry tests
	cd solidity && forge test -v

solidity-deploy: ## Deploy AgentWallet to Sepolia. Usage: make solidity-deploy AGENT=DELTA_GO
	@if [ -z "$(AGENT)" ]; then echo "ERROR: AGENT not set. Usage: make solidity-deploy AGENT=DELTA_GO"; exit 1; fi
	@bash -c 'set -a; source .env; set +a; \
		KEY_VAR="AGENT_$(AGENT)_PRIVATE_KEY"; \
		PRIVATE_KEY=$${!KEY_VAR}; \
		if [ -z "$$PRIVATE_KEY" ]; then echo "ERROR: $$KEY_VAR not set in .env"; exit 1; fi; \
		if [ -z "$$CHAIN_RPC_URL" ]; then echo "ERROR: CHAIN_RPC_URL not set in .env"; exit 1; fi; \
		cd solidity && forge script script/Deploy.s.sol \
			--rpc-url "$$CHAIN_RPC_URL" \
			--private-key "$$PRIVATE_KEY" \
			--broadcast'

# ============================================================
# Rust — simple LLM agent
# ============================================================

rust-build: ## Build Rust agent (requires cargo)
	cd rust && cargo build --release

rust-test: ## Run Rust tests (requires cargo)
	cd rust && cargo test

rust-lint: ## Lint Rust code (requires cargo)
	cd rust && cargo clippy -- -D warnings

rust-docker-build: ## Build Rust agent Docker image
	docker build -t ai-trading-agents-rust:latest -f rust/Dockerfile rust/

rust-docker-test: ## Run Rust tests in Docker (no local Rust needed)
	docker run --rm -w /app -v "$$(pwd)/rust/src:/app/src" -v "$$(pwd)/rust/Cargo.toml:/app/Cargo.toml" rust:1.85-alpine sh -c "apk add --no-cache musl-dev pkgconfig openssl-dev openssl-libs-static && cargo test"

# ============================================================
# Ruby — agent SDK + example agents
# ============================================================

ruby-install: ## Install Ruby dependencies
	@if [ -f ruby/Gemfile ]; then cd ruby && bundle install; else echo "ruby: no Gemfile, skipping"; fi

ruby-test: ## Run Ruby tests
	@if [ -f ruby/Gemfile ]; then cd ruby && bundle exec rake test; else echo "ruby: no Gemfile, skipping"; fi

ruby-lint: ## Lint Ruby code
	@if [ -f ruby/Gemfile ]; then cd ruby && bundle exec rubocop; else echo "ruby: no Gemfile, skipping"; fi

ruby-run-alpha: ## Run primary agent locally
	cd ruby && bundle exec ruby agents/alpha/agent.rb

ruby-run-beta: ## Run second agent locally
	cd ruby && bundle exec ruby agents/beta/agent.rb

# ============================================================
# Agents Arena (Ruby)
# ============================================================

demo-ruby: ## Start Ruby Agents Arena (build + up + migrate)
	$(MAKE) -C ruby/agents/solid_loop_trading build
	$(MAKE) -C ruby/agents/solid_loop_trading up
	@echo "Waiting for database to be ready..."
	@sleep 5
	$(MAKE) -C ruby/agents/solid_loop_trading migrate

ruby-down: ## Stop Ruby Agents Arena
	$(MAKE) -C ruby/agents/solid_loop_trading down

# ============================================================
# Java — Gamma Fear & Greed Contrarian agent
# ============================================================

java-build: ## Build Java agent Docker image
	docker build \
		-t agent-gamma-java:local \
		--label org.opencontainers.image.source=$(REPO_URL) \
		./java

java-test: ## Run Java agent tests (Gradle)
	cd java && ./gradlew test

demo-java: ## Start infra + Java Gamma agent (build from source)
	@mkdir -p data/workspace
	docker compose --profile java up --build -d

java-down: ## Stop Java Gamma agent
	docker compose --profile java stop agent-gamma-java
	docker compose --profile java rm -f agent-gamma-java

# ============================================================
# Python — agent SDK + example agents
# ============================================================

python-install: ## Install Python dependencies
	cd python && make install

python-test: ## Run Python tests
	cd python && make test

python-lint: ## Lint Python code
	cd python && make lint

python-run-beta: ## Run Agent Beta locally
	cd python && python -m agents.beta.agent

# ============================================================
# TypeScript — agent dashboard UI
# ============================================================

typescript-install: ## Install TypeScript dependencies
	cd typescript && npm install

typescript-dev: ## Start dashboard dev server
	cd typescript && npm run dev

typescript-build: ## Build dashboard for production
	cd typescript && npm run build

typescript-test: ## Run TypeScript tests
	cd typescript && npx vitest run

typescript-lint: ## Lint TypeScript code
	cd typescript && npm run lint

# ============================================================
# Database
# ============================================================

postgres-migrate: ## Run Agent DB migrations
	docker compose run --rm trading-pg-migrations

postgres-migrate-down: ## Rollback last Agent DB migration
	docker compose run --rm trading-pg-migrations -path=/migrations -database "postgres://trading:trading@postgres:5432/trading?sslmode=disable" down 1

postgres-reset: ## Reset Agent DB (drop + re-migrate)
	docker compose run --rm trading-pg-migrations -path=/migrations -database "postgres://trading:trading@postgres:5432/trading?sslmode=disable" drop -f
	docker compose run --rm trading-pg-migrations

# ============================================================
# Aggregate
# ============================================================

build: golang-build ruby-install python-install typescript-install typescript-build ## Build everything

test: golang-test golang-test-integration ruby-test python-test typescript-test ## Run all tests (integration tests skip gracefully without Docker/sandbox image)

lint: golang-lint ruby-lint python-lint typescript-lint ## Lint all code

clean: ## Clean build artifacts
	rm -rf golang/bin/
	rm -rf typescript/dist/
	rm -rf rust/target/
