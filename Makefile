.PHONY: all build build-linux build-linux-arm64 build-windows build-darwin build-darwin-arm64 build-all test update-golden test-coverage install clean deps tidy run lint fmt vet check screenshots help

# Binary name
BINARY=ficha

# Build directories
BIN_DIR=bin

# Go parameters
GOCMD=go
# Static (no cgo) so the Linux binaries run on musl distros too; -trimpath keeps
# the builder's filesystem paths out of the binary. Tests keep cgo because
# -race needs it.
GOBUILD=CGO_ENABLED=0 $(GOCMD) build -trimpath
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod

# Version from VERSION file
VERSION=$(shell cat VERSION 2>/dev/null || echo "dev")

# Build flags
LDFLAGS=-ldflags "-s -w -X github.com/bardisty/ficha/cmd.Version=$(VERSION)"

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

# Regenerate golden files after an intentional rendering change, then review the diff
update-golden:
	$(GOTEST) ./internal/formatter ./internal/tui ./cmd -run 'TestGolden' -update

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

# Run go vet standalone. The gate (check) enforces the same analyzers through
# golangci-lint's govet, so running this separately is only for convenience.
vet:
	$(GOCMD) vet ./...

# Run all checks
check: fmt lint test

# Regenerate the README screenshots from a synthetic fixture. Needs vhs
# (charmbracelet/vhs) with its ttyd and ffmpeg dependencies, python3, and the
# DejaVu Sans Mono font the tapes are sized for.
screenshots: build
	@fc-list | grep -q "DejaVu Sans Mono" || { echo "screenshots: DejaVu Sans Mono is not installed (fc-list)"; exit 1; }
	rm -rf $(BIN_DIR)/screenshots
	python3 docs/screenshots/mkfixture.py $(BIN_DIR)/screenshots/fixture
	vhs docs/screenshots/watch.tape
	vhs docs/screenshots/breakdown.tape
	for v in watch breakdown; do \
		ffmpeg -loglevel error -y -i $(BIN_DIR)/screenshots/$$v.png -c:v libwebp -lossless 1 -compression_level 6 docs/ficha-$$v.webp || exit 1; \
	done

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
	@echo "  update-golden   - Regenerate .golden rendering snapshots"
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
	@echo "  screenshots     - Regenerate the README screenshots (needs vhs)"
