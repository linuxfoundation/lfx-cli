# Copyright The Linux Foundation and each contributor to LFX.
# SPDX-License-Identifier: MIT

.PHONY: all build clean check fmt vet lint revive goreleaser-check test test-coverage run deps install-tools megalinter help

# Build variables
BINARY_NAME=lfx
CMD_DIR=./cmd/lfx
BUILD_DIR=./bin

# Build flags. Version is not injected via -ldflags: `go build` already
# embeds a pseudo-version (encoding the VCS revision, commit time, and
# dirty state) into the binary's module version, which main.go reads via
# debug.ReadBuildInfo() as a fallback. A local `make build` therefore
# reports a traceable pseudo-version like v0.0.0-<time>-<revision>[+dirty],
# while `go install ...@vX.Y.Z` reports the resolved tag instead; the
# formats differ, but both identify the exact source that was built.
LDFLAGS=-ldflags="-s -w"

# Default target. Recursive $(MAKE) calls serialize the three phases so
# `make -j all` reliably leaves a checked, up-to-date binary even under
# parallel make (clean/build/check would otherwise race on bin/lfx and
# on source files being formatted while read).
all:
	$(MAKE) clean
	$(MAKE) check
	$(MAKE) build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)

# Run all checks. fmt rewrites source files, so it must complete before
# the read-only checks run; those are safe to parallelize with each other.
check:
	$(MAKE) fmt
	$(MAKE) vet lint revive goreleaser-check

# Format Go code
fmt:
	@echo "Formatting Go code..."
	go fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Run golangci-lint (if available)
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

# Run revive (if available)
revive:
	@echo "Running revive..."
	@if command -v revive >/dev/null 2>&1; then \
		revive ./...; \
	else \
		echo "revive not installed, skipping..."; \
	fi

# Validate the GoReleaser config (if available)
goreleaser-check:
	@echo "Validating .goreleaser.yaml..."
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser check; \
	else \
		echo "goreleaser not installed, skipping..."; \
	fi

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Build and run the CLI
run: build
	@echo "Running lfx..."
	$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install github.com/goreleaser/goreleaser/v2@latest
	go install github.com/mgechev/revive@latest
	@gobin="$$(go env GOPATH)/bin"; \
	case ":$$PATH:" in \
		*":$$gobin:"*) ;; \
		*) echo "Warning: $$gobin is not in your PATH; installed tools (golangci-lint, goreleaser, revive) won't be found. Add it to your PATH, e.g.: export PATH=\"$$PATH:$$gobin\"" ;; \
	esac

# Run MegaLinter locally via Docker (matches CI Go flavor at v9.6.0).
# Keep this version in sync with .github/workflows/mega-linter.yml.
megalinter:
	docker pull ghcr.io/oxsecurity/megalinter-go:v9.6.0
	docker run --rm --platform linux/amd64 -v '$(CURDIR):/tmp/lint:rw' ghcr.io/oxsecurity/megalinter-go:v9.6.0

# Show help
help:
	@echo "Available targets:"
	@echo "  all              - Clean, check, and build (default)"
	@echo "  build            - Build the binary"
	@echo "  clean            - Clean build artifacts"
	@echo "  check            - Run all code quality checks"
	@echo "  fmt              - Format Go code"
	@echo "  vet              - Run go vet"
	@echo "  lint             - Run golangci-lint"
	@echo "  revive           - Run revive"
	@echo "  goreleaser-check - Validate .goreleaser.yaml"
	@echo "  test             - Run tests"
	@echo "  test-coverage    - Run tests with coverage report"
	@echo "  run              - Build and run the CLI (pass args via ARGS=...)"
	@echo "  deps             - Download and tidy dependencies"
	@echo "  install-tools    - Install development tools"
	@echo "  megalinter       - Run MegaLinter locally via Docker"
	@echo "  help             - Show this help message"
