# Variables
BINARY_NAME=analytic-sandbox
BUILD_DIR=bin
SERVER_DIR=./cmd/server
DOCKER_IMAGE_SANDBOX=analytic-sandbox:latest
DOCKER_IMAGE_SERVER=analytic-server:latest

# Default target
all: build

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(SERVER_DIR)

test:
	go test ./...

clean:
	rm -rf $(BUILD_DIR)/
	rm -f $(BINARY_NAME)

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

run1: build
	./$(BUILD_DIR)/$(BINARY_NAME) --addr :8081

integration-test-curl: build
	@if [ -z "$(FILE)" ]; then echo "Usage: make integration-test-curl FILE=<path>"; exit 1; fi
	./$(BUILD_DIR)/$(BINARY_NAME) & PID=$$!; sleep 2; ./scripts/tool_suite_test.sh $(FILE); kill $$PID

docker-build-sandbox:
	docker build -f Dockerfile.sandbox -t $(DOCKER_IMAGE_SANDBOX) .

docker-build-server:
	docker build -t $(DOCKER_IMAGE_SERVER) .

docker-build-all: docker-build-sandbox docker-build-server

docker-clean:
	docker ps -a -q --filter "label=managed-by=analytic-sandbox" | xargs -r docker rm -f

health:
	curl -vvv http://localhost:8081/health

status:
	curl -H "Authorization: Bearer $$MCP_AUTH_TOKEN" http://localhost:8081/status | jq
