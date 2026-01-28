# SPDX-FileCopyrightText: 2026 Zextras <https://www.zextras.com>
#
# SPDX-License-Identifier: AGPL-3.0-only

# Makefile for building service-discover packages using YAP
#
# Usage:
#   make build TARGET=ubuntu-jammy           # Build packages for Ubuntu 22.04
#   make test                                # Run tests
#   make clean                               # Clean build artifacts
#
# Supported targets:
#   ubuntu-jammy, ubuntu-noble, rocky-8, rocky-9

# Configuration
.DEFAULT_GOAL := help
YAP_IMAGE_PREFIX ?= docker.io/m0rf30/yap
YAP_VERSION ?= 1.47
CONTAINER_RUNTIME ?= $(shell command -v podman >/dev/null 2>&1 && echo podman || command -v docker >/dev/null 2>&1 && echo docker || echo podman)

# Build directories
OUTPUT_DIR ?= artifacts
BIN_DIR ?= bin

# CCache directory for build caching
CCACHE_DIR ?= $(CURDIR)/.ccache

# Default target (can be overridden)
TARGET ?= ubuntu-jammy

# Container image name (format: docker.io/m0rf30/yap-<target>:<version>)
YAP_IMAGE = $(YAP_IMAGE_PREFIX)-$(TARGET):$(YAP_VERSION)

# Container name
CONTAINER_NAME ?= yap-$(TARGET)

# Container options
CONTAINER_OPTS = --rm -ti \
	--name $(CONTAINER_NAME) \
	--entrypoint bash \
	-v $(CURDIR):/project \
	-v $(CURDIR)/$(OUTPUT_DIR):/artifacts \
	-v $(CCACHE_DIR):/root/.ccache \
	-e CCACHE_DIR=/root/.ccache

# Go build options
GO ?= go
GOFLAGS ?=
BINARIES = agent server service-discoverd

.PHONY: all build build-packages build-binaries test clean clean-all pull list-targets help install-deps

# Default target
all: help

## help: Show this help message
help:
	@echo "Service Discover - Build System"
	@echo ""
	@echo "This Makefile builds service-discover packages using YAP"
	@echo "(Yet Another Packager) in Podman/Docker containers."
	@echo ""
	@echo "Usage:"
	@echo "  make <target> [TARGET=<distro>] [OPTIONS]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /' | column -t -s ':'
	@echo ""
	@echo "Options:"
	@echo "  TARGET             Distribution target (default: $(TARGET))"
	@echo "  YAP_IMAGE_PREFIX   YAP image prefix (default: $(YAP_IMAGE_PREFIX))"
	@echo "  YAP_VERSION        YAP image version (default: $(YAP_VERSION))"
	@echo "  CONTAINER_RUNTIME  Container runtime (default: podman)"
	@echo "  CONTAINER_NAME     Container name (default: $(CONTAINER_NAME))"
	@echo "  OUTPUT_DIR         Output directory for packages (default: $(OUTPUT_DIR))"
	@echo "  CCACHE_DIR         CCache directory for build caching (default: $(CCACHE_DIR))"
	@echo "  BIN_DIR            Binary output directory (default: $(BIN_DIR))"
	@echo ""
	@echo "Examples:"
	@echo "  make build TARGET=ubuntu-jammy"
	@echo "  make build-binaries"
	@echo "  make test"
	@echo "  make pull TARGET=ubuntu-noble"
	@echo ""

## install-deps: Install Go dependencies
install-deps:
	@echo "Installing Go dependencies..."
	$(GO) mod download

## build-binaries: Build Go binaries locally
build-binaries:
	@echo "Building Go binaries..."
	@mkdir -p $(BIN_DIR)
	@for binary in $(BINARIES); do \
		echo "Building $$binary..."; \
		$(GO) build $(GOFLAGS) -o $(BIN_DIR)/$$binary ./cmd/$$binary || exit 1; \
	done
	@echo "Binaries built successfully in $(BIN_DIR)/"

## build: Build packages for the specified TARGET using YAP
build: build-packages

## build-packages: Build distribution packages using YAP
build-packages:
	@echo "Building packages for $(TARGET)..."
	@mkdir -p $(OUTPUT_DIR) $(CCACHE_DIR)
	$(CONTAINER_RUNTIME) run $(CONTAINER_OPTS) $(YAP_IMAGE) -c "yap prepare $(TARGET) -g && yap build $(TARGET) /project/build -sd"

## test: Run tests
test:
	@echo "Running tests..."
	$(GO) run gotest.tools/gotestsum@latest --format testname --junitfile tests.xml ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	$(GO) test -v ./...

## pull: Pull the YAP container image for the specified TARGET
pull:
	@echo "Pulling YAP image for $(TARGET)..."
	$(CONTAINER_RUNTIME) pull $(YAP_IMAGE)

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(OUTPUT_DIR)
	rm -rf $(BIN_DIR)
	rm -f tests.xml

## clean-all: Remove all build artifacts including cache
clean-all: clean
	@echo "Cleaning cache..."
	rm -rf $(CCACHE_DIR)

## list-targets: List supported distribution targets
list-targets:
	@echo "Supported distribution targets:"
	@echo ""
	@echo "  ubuntu-jammy    (Ubuntu 22.04 LTS)"
	@echo "  ubuntu-noble    (Ubuntu 24.04 LTS)"
	@echo "  rocky-8         (Rocky Linux 8)"
	@echo "  rocky-9         (Rocky Linux 9)"
	@echo ""
	@echo "Usage: make build TARGET=<target>"

## lint: Run Go linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Install it from https://golangci-lint.run/usage/install/"; \
		exit 1; \
	fi
