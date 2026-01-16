# Makefile for service-discover

# Variables
PROJECT_NAME=service-discover
AGENT_BINARY=agent
SERVER_BINARY=server
SERVICE_DISCOVERD_BINARY=service-discoverd
BUILD_DIR=./bin
AGENT_PATH=./cmd/agent
SERVER_PATH=./cmd/server
SERVICE_DISCOVERD_PATH=./cmd/service-discoverd

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOLINT=golangci-lint

# Build flags
LDFLAGS=-ldflags="-s -w"
BUILD_FLAGS=-trimpath $(LDFLAGS)

.PHONY: all build build-agent build-server build-service-discoverd clean test deps fmt lint help install run-tests

# Default target
all: clean deps fmt lint test build

# Build all binaries
build: build-agent build-server build-service-discoverd

# Build agent binary
build-agent:
	@echo "Building $(AGENT_BINARY)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(AGENT_BINARY) $(AGENT_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(AGENT_BINARY)"

# Build server binary
build-server:
	@echo "Building $(SERVER_BINARY)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) $(SERVER_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(SERVER_BINARY)"

# Build service-discoverd binary
build-service-discoverd:
	@echo "Building $(SERVICE_DISCOVERD_BINARY)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(SERVICE_DISCOVERD_BINARY) $(SERVICE_DISCOVERD_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(SERVICE_DISCOVERD_BINARY)"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@rm -f tests.xml
	@echo "Clean complete"

# Run tests with gotestsum
test:
	@echo "Running tests with gotestsum..."
	@$(GOCMD) run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml ./...

# Run tests (alternative without gotestsum)
test-simple:
	@echo "Running tests..."
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Lint code
lint:
	@echo "Linting code..."
	@if command -v $(GOLINT) > /dev/null; then \
		$(GOLINT) run ./...; \
	else \
		echo "golangci-lint not installed, skipping lint"; \
	fi

# Run agent
run-agent: build-agent
	@echo "Running $(AGENT_BINARY)..."
	$(BUILD_DIR)/$(AGENT_BINARY)

# Run server
run-server: build-server
	@echo "Running $(SERVER_BINARY)..."
	$(BUILD_DIR)/$(SERVER_BINARY)

# Run service-discoverd
run-service-discoverd: build-service-discoverd
	@echo "Running $(SERVICE_DISCOVERD_BINARY)..."
	$(BUILD_DIR)/$(SERVICE_DISCOVERD_BINARY)

# Install to system
install: build
	@echo "Installing binaries to /opt/zextras/libexec/..."
	@sudo mkdir -p /opt/zextras/libexec/
	@sudo cp $(BUILD_DIR)/$(AGENT_BINARY) /opt/zextras/libexec/
	@sudo cp $(BUILD_DIR)/$(SERVER_BINARY) /opt/zextras/libexec/
	@sudo cp $(BUILD_DIR)/$(SERVICE_DISCOVERD_BINARY) /opt/zextras/libexec/
	@sudo chown zextras:zextras /opt/zextras/libexec/$(AGENT_BINARY)
	@sudo chown zextras:zextras /opt/zextras/libexec/$(SERVER_BINARY)
	@sudo chown zextras:zextras /opt/zextras/libexec/$(SERVICE_DISCOVERD_BINARY)
	@sudo chmod 755 /opt/zextras/libexec/$(AGENT_BINARY)
	@sudo chmod 755 /opt/zextras/libexec/$(SERVER_BINARY)
	@sudo chmod 755 /opt/zextras/libexec/$(SERVICE_DISCOVERD_BINARY)
	@echo "Installation complete"

# Build for different architectures - agent
build-agent-linux-amd64:
	@echo "Building agent for Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(AGENT_BINARY)-linux-amd64 $(AGENT_PATH)

build-agent-linux-arm64:
	@echo "Building agent for Linux ARM64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(AGENT_BINARY)-linux-arm64 $(AGENT_PATH)

# Build for different architectures - server
build-server-linux-amd64:
	@echo "Building server for Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-linux-amd64 $(SERVER_PATH)

build-server-linux-arm64:
	@echo "Building server for Linux ARM64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY)-linux-arm64 $(SERVER_PATH)

# Build for different architectures - service-discoverd
build-service-discoverd-linux-amd64:
	@echo "Building service-discoverd for Linux AMD64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(SERVICE_DISCOVERD_BINARY)-linux-amd64 $(SERVICE_DISCOVERD_PATH)

build-service-discoverd-linux-arm64:
	@echo "Building service-discoverd for Linux ARM64..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(BUILD_FLAGS) -o $(BUILD_DIR)/$(SERVICE_DISCOVERD_BINARY)-linux-arm64 $(SERVICE_DISCOVERD_PATH)

# Build for all supported architectures
build-all: build-agent-linux-amd64 build-agent-linux-arm64 build-server-linux-amd64 build-server-linux-arm64 build-service-discoverd-linux-amd64 build-service-discoverd-linux-arm64

# Security scan
security:
	@echo "Running security scan..."
	@if command -v gosec > /dev/null; then \
		gosec ./... -quiet || echo "Security scan completed with warnings"; \
	else \
		echo "gosec not installed, skipping security scan"; \
	fi

# Benchmark tests
benchmark:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Update dependencies
update-deps:
	@echo "Updating dependencies..."
	$(GOMOD) get -u ./...
	$(GOMOD) tidy

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify

# Check for outdated dependencies
outdated:
	@echo "Checking for outdated dependencies..."
	@$(GOCMD) list -u -m all

# Create a release
release: clean deps fmt lint test build-all
	@echo "Creating release..."
	@mkdir -p releases
	@tar -czf releases/$(AGENT_BINARY)-linux-amd64.tar.gz -C $(BUILD_DIR) $(AGENT_BINARY)-linux-amd64
	@tar -czf releases/$(AGENT_BINARY)-linux-arm64.tar.gz -C $(BUILD_DIR) $(AGENT_BINARY)-linux-arm64
	@tar -czf releases/$(SERVER_BINARY)-linux-amd64.tar.gz -C $(BUILD_DIR) $(SERVER_BINARY)-linux-amd64
	@tar -czf releases/$(SERVER_BINARY)-linux-arm64.tar.gz -C $(BUILD_DIR) $(SERVER_BINARY)-linux-arm64
	@tar -czf releases/$(SERVICE_DISCOVERD_BINARY)-linux-amd64.tar.gz -C $(BUILD_DIR) $(SERVICE_DISCOVERD_BINARY)-linux-amd64
	@tar -czf releases/$(SERVICE_DISCOVERD_BINARY)-linux-arm64.tar.gz -C $(BUILD_DIR) $(SERVICE_DISCOVERD_BINARY)-linux-arm64
	@echo "Release packages created in releases/"

# Build packages using build_packages.sh
packages:
	@echo "Building packages..."
	@if [ -f build_packages.sh ]; then \
		./build_packages.sh; \
	else \
		echo "build_packages.sh not found"; \
	fi

# Development server with auto-reload (requires air)
dev:
	@echo "Starting development server..."
	@if command -v air > /dev/null; then \
		air; \
	else \
		echo "air not installed. Install with: go install github.com/cosmtrek/air@latest"; \
		echo "Falling back to regular run..."; \
		$(MAKE) run-server; \
	fi

# Help
help:
	@echo "Available targets for $(PROJECT_NAME):"
	@echo "  all                - Clean, deps, fmt, lint, test, and build all binaries"
	@echo "  build              - Build all binaries (agent, server, service-discoverd)"
	@echo "  build-agent        - Build agent binary only"
	@echo "  build-server       - Build server binary only"
	@echo "  build-service-discoverd - Build service-discoverd binary only"
	@echo "  clean              - Clean build artifacts"
	@echo "  test               - Run tests with gotestsum"
	@echo "  test-simple        - Run tests without gotestsum"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  deps               - Download dependencies"
	@echo "  fmt                - Format code"
	@echo "  lint               - Lint code with golangci-lint"
	@echo "  run-agent          - Build and run agent"
	@echo "  run-server         - Build and run server"
	@echo "  run-service-discoverd - Build and run service-discoverd"
	@echo "  install            - Install binaries to /opt/zextras/libexec/"
	@echo "  build-all          - Build for all supported architectures"
	@echo "  security           - Run security scan with gosec"
	@echo "  benchmark          - Run benchmark tests"
	@echo "  update-deps        - Update dependencies"
	@echo "  verify             - Verify dependencies"
	@echo "  release            - Create release packages"
	@echo "  packages           - Build .deb/.rpm packages using build_packages.sh"
	@echo "  dev                - Start development server with auto-reload"
	@echo "  help               - Show this help"
