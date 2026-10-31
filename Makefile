.PHONY: all build build-bin build-mcp version docker-build-mcp dev test lint fmt vet clean css css-check templ migrate help

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOFMT=gofmt
GOMOD=$(GOCMD) mod
BINARY_NAME=goforge
BINARY_PATH=bin/$(BINARY_NAME)

# Tools
TEMPL=go tool templ
# Tailwind CLI. Must be v$(TAILWIND_VERSION), the version the Dockerfile and the
# CI css job use; v4 cannot read tailwind.config.js. Empty TAILWIND picks a
# standalone `tailwindcss` on PATH if it is v$(TAILWIND_VERSION), else
# `npx --yes tailwindcss@$(TAILWIND_VERSION)`. css, css-watch, and css-check fail
# (no placeholder, no stale output.css) when no v$(TAILWIND_VERSION) CLI runs.
TAILWIND?=
TAILWIND_VERSION=3.4.17
# Shell snippet: sets $$tw to a v$(TAILWIND_VERSION) Tailwind command or exits 1.
TAILWIND_RESOLVE=set -e; tw="$(TAILWIND)"; \
	if [ -z "$$tw" ]; then \
		if command -v tailwindcss >/dev/null 2>&1 && tailwindcss --help 2>&1 | grep -q "v$(TAILWIND_VERSION)"; then \
			tw=tailwindcss; \
		elif command -v npx >/dev/null 2>&1; then \
			tw="npx --yes tailwindcss@$(TAILWIND_VERSION)"; \
		else \
			echo "error: Tailwind v$(TAILWIND_VERSION) not found: install the standalone CLI (make install-tools) or Node (for npx)" >&2; exit 1; \
		fi; \
	fi; \
	if ! $$tw --help 2>&1 | grep -q "v$(TAILWIND_VERSION)"; then \
		echo "error: '$$tw' is not Tailwind v$(TAILWIND_VERSION) (Dockerfile and CI use v$(TAILWIND_VERSION)); set TAILWIND=\"npx --yes tailwindcss@$(TAILWIND_VERSION)\"" >&2; exit 1; \
	fi
AIR=air
GOLINT=go tool golangci-lint

# Version: the git tag (v0.1.0, or v0.1.0-3-gabc1234[-dirty] past a tag), or
# "dev" with no tag or no .git. Override with `make build VERSION=v0.1.0`.
VERSION?=$(shell git describe --tags --dirty 2>/dev/null || echo dev)
MODULE=github.com/raythurman2386/goforge

# Build flags. -X sets main.version (goforge --version, /api/v1/health, logs,
# backup manifests) and mcpserver.Version (goforge-mcp --version, MCP clients).
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X $(MODULE)/internal/mcpserver.Version=$(VERSION)"

# Default target
all: build

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

## build: Build production binary
build: templ css
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/goforge

## version: Print the version make build stamps into the binaries
version:
	@echo $(VERSION)

## build-bin: Build the goforge binary only (templ and CSS already generated; honors GOOS/GOARCH)
build-bin:
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/goforge

## build-mcp: Build the GoForge MCP server (stdio)
build-mcp:
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o bin/goforge-mcp ./cmd/goforge-mcp

## dev: Run with hot reload (requires air)
dev:
	GOFORGE_DEV_MODE=true $(AIR)

## run: Run the server directly
run: templ
	GOFORGE_DEV_MODE=true $(GOCMD) run ./cmd/goforge

## test: Run all tests
test:
	$(GOTEST) -v -race -cover ./...

## test-short: Run tests without race detector
test-short:
	$(GOTEST) -v -cover ./...

## test-coverage: Generate coverage report
test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run golangci-lint
lint:
	$(GOLINT) run ./...

## vet: Run go vet
vet:
	$(GOVET) ./...

## fmt: Format code
fmt:
	$(GOFMT) -s -w .

## fmt-check: Check code formatting (for CI)
fmt-check:
	@test -z "$$($(GOFMT) -l .)" || (echo "Code not formatted. Run 'make fmt'" && exit 1)

## templ: Generate templ templates
templ:
	$(TEMPL) generate

## templ-watch: Watch and regenerate templ files
templ-watch:
	$(TEMPL) generate --watch

## css: Compile TailwindCSS
css:
	@$(TAILWIND_RESOLVE); \
	echo "$$tw -i ./static/css/input.css -o ./static/css/output.css --minify"; \
	$$tw -i ./static/css/input.css -o ./static/css/output.css --minify

## css-check: Rebuild CSS from scratch and fail on class tokens missing from output.css
css-check:
	rm -f static/css/output.css
	@$(MAKE) --no-print-directory css
	python3 scripts/class_check.py static/css/output.css

## css-watch: Watch and compile TailwindCSS
css-watch:
	@$(TAILWIND_RESOLVE); \
	$$tw -i ./static/css/input.css -o ./static/css/output.css --watch

## migrate: Run database migrations up
migrate:
	$(GOCMD) run ./cmd/goforge migrate up

## migrate-down: Rollback last migration
migrate-down:
	$(GOCMD) run ./cmd/goforge migrate down 1

## migrate-status: Show migration status
migrate-status:
	$(GOCMD) run ./cmd/goforge migrate status

## migrate-create: Create new migration (usage: make migrate-create name=create_users)
migrate-create:
	@test -n "$(name)" || (echo "Usage: make migrate-create name=migration_name" && exit 1)
	$(GOCMD) run ./cmd/goforge migrate create $(name)

## tidy: Tidy and verify dependencies
tidy:
	$(GOMOD) tidy
	$(GOMOD) verify

## deps: Download dependencies
deps:
	$(GOMOD) download

## clean: Remove build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html
	rm -f static/css/output.css
	find . -name '*_templ.go' -delete

## install-tools: Install development tools
install-tools:
	go install github.com/air-verse/air@latest
	@echo "Tools (templ, golangci-lint) are managed via go.mod and 'go tool'."
	@echo "Run 'go mod download' to ensure they are available."
	@echo ""
	@echo "TailwindCSS v$(TAILWIND_VERSION) must be installed manually (not v4):"
	@echo "  curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/download/v$(TAILWIND_VERSION)/tailwindcss-linux-x64"
	@echo "  chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss"

## docker-build: Build Docker image
docker-build:
	docker build -t goforge:latest .

## docker-build-mcp: Build the MCP server image (target mcp)
docker-build-mcp:
	docker build --target mcp -t goforge-mcp:latest .

## docker-run: Run in Docker
docker-run:
	docker run --rm -p 8080:8080 --env-file .env goforge:latest

## check: Run all checks (fmt, vet, lint, test)
check: fmt-check vet lint test
	@echo "All checks passed!"

## ci: Run CI pipeline locally (needs Tailwind v3.4.17 for css-check)
ci: deps templ css-check fmt-check vet lint test
	@echo "CI pipeline passed!"
