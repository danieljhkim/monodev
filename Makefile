.PHONY: help build install clean test format all

# Default target
.DEFAULT_GOAL := help

# Build variables
BINARY_NAME := monodev
BUILD_DIR := bin
GO_FILES := $(shell find . -name '*.go' -not -path './vendor/*')
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Go build flags
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

##@ General

help: ## Display this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

all: clean build ## Clean and build the binary

build: ## Build the Go binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) cmd/monodev/main.go
	@echo "✓ Built $(BUILD_DIR)/$(BINARY_NAME)"

install: build ## Build and install to /usr/local/bin
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@echo "✓ Installed to /usr/local/bin/$(BINARY_NAME)"

clean: ## Remove build artifacts and test coverage files
	@echo "Cleaning build artifacts and test files..."
	rm -rf $(BUILD_DIR) coverage.out coverage.html
	@echo "✓ Cleaned"

##@ Testing

test: ## Run unit tests (fast, no system dependencies)
	@echo "Running unit tests..."
	go test -v ./internal/...


test-quick: ## Run tests without verbose output (faster)
	@go test ./internal/...

##@ Development

format: ## Format Go code
	@echo "Formatting Go code..."
	gofmt -w $(GO_FILES)
	@echo "✓ Formatted"

lint: ## Run golangci-lint (requires golangci-lint)
	@command -v golangci-lint >/dev/null 2>&1 || { echo "ERROR: golangci-lint not found. Install it: brew install golangci-lint" >&2; exit 1; }
	golangci-lint run ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...
	@echo "✓ Vet passed"

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy
	@echo "✓ Dependencies updated"


##@ Information

version: ## Show version information
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"

size: build ## Show binary size
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME) | awk '{print "Binary size:", $$5}'
