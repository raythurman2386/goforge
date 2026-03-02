.PHONY: all build dev test lint fmt vet clean css templ migrate help

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
TAILWIND=tailwindcss
AIR=air
GOLINT=go tool golangci-lint

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
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_PATH) ./cmd/goforge

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
	@if command -v $(TAILWIND) >/dev/null 2>&1; then \
		$(TAILWIND) -i ./static/css/input.css -o ./static/css/output.css --minify; \
	else \
		echo "tailwindcss not installed, using placeholder CSS"; \
	fi

## css-watch: Watch and compile TailwindCSS
css-watch:
	$(TAILWIND) -i ./static/css/input.css -o ./static/css/output.css --watch

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
	@echo "TailwindCSS must be installed manually:"
	@echo "  curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64"
	@echo "  chmod +x tailwindcss-linux-x64 && sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss"

## docker-build: Build Docker image
docker-build:
	docker build -t goforge:latest .

## docker-run: Run in Docker
docker-run:
	docker run --rm -p 8080:8080 --env-file .env goforge:latest

## check: Run all checks (fmt, vet, lint, test)
check: fmt-check vet lint test
	@echo "All checks passed!"

## ci: Run CI pipeline locally
ci: deps templ css fmt-check vet lint test
	@echo "CI pipeline passed!"
