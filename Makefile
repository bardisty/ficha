.PHONY: all build build-linux build-linux-arm64 build-windows build-darwin build-darwin-arm64 build-all test update-golden test-coverage install clean deps tidy run lint fmt fmt-check vet check test-race ci fixture screenshots help

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
# Extra go test flags, e.g. TESTFLAGS=-v or TESTFLAGS='-run TestGolden'
TESTFLAGS=
GOMOD=$(GOCMD) mod

# golangci-lint v1.64.8 can't read the standard library's export data from Go
# 1.27 on, so it has to build and load packages with an older Go, whatever is
# installed locally. GOTOOLCHAIN pins both go run's build and the linter's own
# go list calls to go.mod's toolchain, or its go line when there is no
# toolchain line.
GOLANGCI_LINT_VERSION=v1.64.8
LINT_GOTOOLCHAIN=$(shell awk '$$1 == "go" { g = "go" $$2 } $$1 == "toolchain" { t = $$2 } END { print (t != "" ? t : g) }' go.mod)

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
	$(GOTEST) $(TESTFLAGS) ./...

# Regenerate golden files after an intentional rendering change, then review the diff
update-golden:
	$(GOTEST) ./internal/formatter ./internal/tui ./cmd -run 'TestGolden' -update

# Run tests with coverage
test-coverage:
	$(GOTEST) $(TESTFLAGS) -coverprofile=coverage.out ./...
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

# Run golangci-lint, building the pinned version on first use
lint:
	GOTOOLCHAIN=$(LINT_GOTOOLCHAIN) $(GOCMD) run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

# Lists every Go file, skipping hidden files and directories at any depth.
# gofmt, unlike go test and golangci-lint, descends into hidden directories,
# and other worktrees nested under the checkout would get their work in
# progress rewritten or failed.
FIND_GO = find . -path '*/.*' -prune -o -name '*.go' -print

# Format all Go files
fmt:
	$(FIND_GO) | xargs gofmt -w

# Run go vet standalone. The gate (check) enforces the same analyzers through
# golangci-lint's govet, so running this separately is only for convenience.
vet:
	$(GOCMD) vet ./...

# Run all checks
check: fmt lint test

# Fail on unformatted files and name them, without rewriting anything.
fmt-check:
	@files=$$($(FIND_GO) | xargs gofmt -l) || exit 1; \
	if [ -n "$$files" ]; then echo "gofmt would reformat:"; echo "$$files"; exit 1; fi

# Run the tests with the race detector. -race needs cgo and a C compiler, so
# without them this runs plain tests and says why, rather than failing a
# local run over a missing toolchain. In CI, where GitHub sets CI, it fails
# instead: a job that quietly lost cgo would stop checking for races.
test-race:
	@reason=""; \
	cc=$$($(GOCMD) env CC); \
	if ! command -v "$${cc%% *}" >/dev/null 2>&1; then reason="the C compiler $$cc is not on PATH"; \
	elif [ "$$($(GOCMD) env CGO_ENABLED)" != 1 ]; then reason="CGO_ENABLED is 0"; fi; \
	if [ -z "$$reason" ]; then \
		echo "$(GOTEST) -race $(TESTFLAGS) ./..."; $(GOTEST) -race $(TESTFLAGS) ./...; \
	elif [ -n "$$CI" ]; then \
		echo "test-race: -race needs cgo, and $$reason" >&2; exit 1; \
	else \
		echo "test-race: skipping -race because $$reason; running plain tests"; \
		CGO_ENABLED=0 $(GOTEST) $(TESTFLAGS) ./...; \
	fi

# What CI's check job runs, except govulncheck. Slower than check, and it
# never rewrites files.
ci: fmt-check lint test-race build-all

# Where make fixture writes the synthetic transcripts. Set it on the command
# line; the environment can't redirect the rm -rf below.
FIXTURE = $(BIN_DIR)/fixture

# Build the synthetic fixture to run ficha against, with nothing but python3.
# It starts over each time, dropping whatever live.py appended, so it only
# deletes a directory carrying the marker a finished run leaves behind.
fixture:
	@if [ -z "$(FIXTURE)" ]; then echo "fixture: FIXTURE is empty" >&2; exit 1; fi
	@if [ -e "$(FIXTURE)" ] && [ ! -f "$(FIXTURE)/.ficha-fixture" ]; then \
		echo "fixture: $(FIXTURE) exists and isn't a fixture, so it was left alone" >&2; exit 1; \
	fi
	rm -rf "$(FIXTURE)"
	python3 docs/screenshots/mkfixture.py "$(FIXTURE)"
	@touch "$(FIXTURE)/.ficha-fixture"

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
	@echo "  fmt-check       - Fail on unformatted files without rewriting them"
	@echo "  test-race       - Run tests with the race detector (plain tests without cgo)"
	@echo "  check           - Run fmt, lint, and test"
	@echo "  ci              - Run what CI runs: fmt-check, lint, test-race, build-all"
	@echo "  fixture         - Build synthetic transcripts in bin/fixture (FIXTURE=dir)"
	@echo "  screenshots     - Regenerate the README screenshots (needs vhs)"
