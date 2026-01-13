.PHONY: all build dev test lint fmt vet clean css templ migrate help

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOFMT=gofmt
GOMOD=$(GOCMD) mod
BINARY_NAME=gocool
BINARY_PATH=bin/$(BINARY_NAME)

# Tools
TEMPL=templ
TAILWIND=tailwindcss
AIR=air
GOLINT=golangci-lint

# Build flags
LDFLAGS=-ldflags "-s -w"

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
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/gocool

## dev: Run with hot reload (requires air)
dev:
	GOCOOL_DEV_MODE=true $(AIR)

## run: Run the server directly
run: templ
	GOCOOL_DEV_MODE=true $(GOCMD) run ./cmd/gocool

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
	@command -v $(TEMPL) >/dev/null 2>&1 && $(TEMPL) generate || echo "templ not installed, skipping template generation"

## templ-watch: Watch and regenerate templ files
templ-watch:
	$(TEMPL) generate --watch

## css: Compile TailwindCSS
css:
	@command -v $(TAILWIND) >/dev/null 2>&1 && $(TAILWIND) -i ./static/css/input.css -o ./static/css/output.css --minify || echo "tailwindcss not installed, using placeholder CSS"

## css-watch: Watch and compile TailwindCSS
css-watch:
	$(TAILWIND) -i ./static/css/input.css -o ./static/css/output.css --watch

## migrate: Run database migrations up
migrate:
	$(GOCMD) run ./cmd/gocool migrate up

## migrate-down: Rollback last migration
migrate-down:
	$(GOCMD) run ./cmd/gocool migrate down 1

## migrate-status: Show migration status
migrate-status:
	$(GOCMD) run ./cmd/gocool migrate status

## migrate-create: Create new migration (usage: make migrate-create name=create_users)
migrate-create:
	@test -n "$(name)" || (echo "Usage: make migrate-create name=migration_name" && exit 1)
	$(GOCMD) run ./cmd/gocool migrate create $(name)

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
	go install github.com/a-h/templ/cmd/templ@latest
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "TailwindCSS must be installed manually:"
	@echo "  curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64"
	@echo "  chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss"

## docker-build: Build Docker image
docker-build:
	docker build -t gocool:latest .

## docker-run: Run in Docker
docker-run:
	docker run --rm -p 8080:8080 --env-file .env gocool:latest

## check: Run all checks (fmt, vet, lint, test)
check: fmt-check vet lint test
	@echo "All checks passed!"

## ci: Run CI pipeline locally
ci: deps templ css fmt-check vet lint test
	@echo "CI pipeline passed!"
