.PHONY: build build-linux build-windows build-all clean test install lint fmt vet check

# Binary name
BINARY=ccusage

# Build directories
BIN_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags "-s -w"

# Default target
all: build

# Build for current platform
build:
	$(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) .

# Build for Linux AMD64
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-linux-amd64 .

# Build for Linux ARM64
build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-linux-arm64 .

# Build for Windows AMD64
build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-windows-amd64.exe .

# Build for macOS AMD64
build-darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-darwin-amd64 .

# Build for macOS ARM64 (Apple Silicon)
build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-darwin-arm64 .

# Build for all platforms
build-all: build-linux build-linux-arm64 build-windows build-darwin build-darwin-arm64

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Install to GOPATH/bin
install:
	$(GOBUILD) $(LDFLAGS) -o $(shell go env GOPATH)/bin/$(BINARY) .

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	rm -f coverage.out coverage.html

# Download dependencies
deps:
	$(GOMOD) download

# Tidy dependencies
tidy:
	$(GOMOD) tidy

# Run the application
run:
	$(GOCMD) run . $(ARGS)

# Run golangci-lint
lint:
	golangci-lint run ./...

# Format all Go files
fmt:
	gofmt -w .

# Run go vet (informational - has known issues)
vet:
	$(GOCMD) vet ./...

# Run all checks
check: fmt lint test

# Help
help:
	@echo "Available targets:"
	@echo "  build           - Build for current platform"
	@echo "  build-linux     - Build for Linux AMD64"
	@echo "  build-linux-arm64 - Build for Linux ARM64"
	@echo "  build-windows   - Build for Windows AMD64"
	@echo "  build-darwin    - Build for macOS AMD64"
	@echo "  build-darwin-arm64 - Build for macOS ARM64"
	@echo "  build-all       - Build for all platforms"
	@echo "  test            - Run tests"
	@echo "  test-coverage   - Run tests with coverage"
	@echo "  install         - Install to GOPATH/bin"
	@echo "  clean           - Clean build artifacts"
	@echo "  deps            - Download dependencies"
	@echo "  tidy            - Tidy dependencies"
	@echo "  run             - Run the application"
	@echo "  lint            - Run golangci-lint"
	@echo "  fmt             - Format all Go files"
	@echo "  vet             - Run go vet"
	@echo "  check           - Run fmt, lint, and test"
