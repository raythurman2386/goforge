# GoForge Project Context

## Project Overview

**GoForge** is a lightweight, self-hosted Platform as a Service (PaaS) built in Go. It effectively reimplements the core developer experience of [Coolify](https://coolify.io) (git-to-deploy, one-click services, automatic SSL) but focuses on being lightweight and simple.

**Key Features:**
*   **Git-based deployments:** Deploy from GitHub, GitLab, Gitea, or raw SSH.
*   **Container Management:** Orchestrates Docker containers and Docker Compose projects.
*   **One-click Services:** Deploys databases (Postgres, Redis, etc.) and other apps.
*   **Reverse Proxy:** Automates Traefik configuration with Let's Encrypt SSL.
*   **Architecture:** Single binary, server-rendered HTMX frontend (no SPA complexity).

## Technical Stack

*   **Language:** Go 1.25+
*   **Web Framework:** `chi` (router)
*   **Frontend:** `HTMX` (interactivity), `templ` (type-safe HTML), `TailwindCSS` (styling).
*   **Database:** PostgreSQL (using `lib/pq` and `golang-migrate`).
*   **Runtime:** Docker Engine & Docker Compose.
*   **Proxy:** Traefik.
*   **Dev Tools:** `air` (hot reload), `golangci-lint`, `make`.

## Directory Structure

*   `cmd/goforge/`: Application entry point (`main.go`).
*   `internal/`: Core application logic.
    *   `auth/`: Authentication (GitHub OAuth, sessions).
    *   `config/`: Configuration loading.
    *   `database/`: Database connection and migrations.
    *   `deploy/`: Deployment orchestration logic.
    *   `docker/`: Docker SDK wrapper.
    *   `git/`: Git provider integrations.
    *   `models/`: Domain data models.
    *   `web/`: HTTP handlers and UI templates.
        *   `handlers/`: HTTP request handlers.
        *   `templates/`: `templ` definitions (`.templ` files).
*   `static/`: Static assets (CSS, JS, images).
*   `deploy/`: Deployment configurations (Traefik, systemd).
*   `templates/`: YAML definitions for one-click services.

## Development Workflow

### Prerequisites
*   Go 1.25+
*   Docker Engine
*   PostgreSQL
*   `templ` CLI: `go install github.com/a-h/templ/cmd/templ@latest`
*   `air`: `go install github.com/air-verse/air@latest`
*   TailwindCSS CLI

### Key Commands (Makefile)

*   `make dev`: Start the server with hot reload (requires `air`).
*   `make build`: Build the production binary (compiles CSS and templates first).
*   `make templ`: Generate Go code from `.templ` files.
*   `make css`: Compile TailwindCSS.
*   `make migrate`: Run database migrations up.
*   `make test`: Run all tests.
*   `make lint`: Run `golangci-lint`.
*   `make help`: Show all available commands.

### Configuration
Configuration is managed via environment variables (loaded from `.env`).
See `.env.example` for the complete list of variables (database URL, encryption keys, OAuth creds, etc.).

## Coding Conventions

**Refer to `AGENTS.md` for strict guidelines.** Key points:

*   **Formatting:** Standard `gofmt`.
*   **Imports:** Grouped: Standard Lib -> Third Party -> Local (`github.com/raythurman2386/goforge/...`).
*   **Errors:** Always wrap errors: `fmt.Errorf("failed to x: %w", err)`. Use sentinel errors (`ErrNotFound`) where appropriate.
*   **Context:** Pass `context.Context` as the first argument to functions performing I/O.
*   **Frontend:**
    *   Use `templ` for all HTML rendering.
    *   Use HTMX for dynamic behavior; avoid inline JavaScript.
    *   Style with TailwindCSS utility classes.

## Architecture Notes

*   **Hypermedia-Driven:** The UI is server-side rendered. HTMX swaps HTML fragments.
*   **Service Layer:** Business logic resides in `internal/services/` or specific package services (e.g., `internal/deploy/service.go`), separating concerns from HTTP handlers.
*   **Repository Pattern:** Database access is abstracted via repositories in `internal/database/repositories/`.
*   **Deployment State Machine:** Deployments follow a strict state flow (pending -> cloning -> building -> deploying -> running/failed).

## Documentation Resources
*   `SPECIFICATION.md`: Detailed technical design and requirements.
*   `AGENTS.md`: Specific instructions for AI agents modifying this codebase.
*   `CONTRIBUTING.md`: General contribution guidelines.

* Go version must NEVER be dropped below the official latest release (currently 1.25).
