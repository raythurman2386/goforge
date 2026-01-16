# GoForge

GoForge is a lightweight, self-hosted Platform as a Service (PaaS) built in Go.

## Features

- **Authentication:** Local email/password and GitHub OAuth.
- **Project Management:** CRUD operations for projects and environments.
- **Docker Integration:** Build and deploy containers directly from git repositories.
- **Traefik Integration:** Automatic routing with Docker labels.
- **Monitoring:** Real-time container resource monitoring (CPU, Memory).
- **Real-time Updates:** SSE-powered deployment logs and status updates.

## Getting Started

### Prerequisites

- Go 1.25+
- Docker Engine
- SQLite or PostgreSQL
- `templ` CLI (`go install github.com/a-h/templ/cmd/templ@latest`)

### Setup

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/raythurman2386/goforge.git
    cd goforge
    ```

2.  **Install dependencies:**
    ```bash
    go mod download
    ```

3.  **Configure environment:**
    Create a `.env` file or use environment variables. In development mode, secrets are generated automatically.
    ```bash
    export GOFORGE_DEV_MODE=true
    ```

4.  **Run migrations:**
    ```bash
    make migrate
    ```

5.  **Start the development server:**
    ```bash
    make dev
    ```
    The server will be available at `http://localhost:8080`.

## Commands

- `make build`: Build the production binary.
- `make dev`: Run with hot reload (requires `air`).
- `make test`: Run all tests.
- `make lint`: Run golangci-lint.
- `make templ`: Generate templ templates.

## Architecture

GoForge follows a clean architecture with:
- **Cmd:** Entry point and CLI commands.
- **Internal:** Core logic separated into `auth`, `project`, `docker`, `git`, `deploy`, `proxy`, `sse`, and `monitor`.
- **Web:** HTMX + Templ frontend with a RESTful-ish backend.
- **Database:** Repository pattern supporting SQLite and Postgres.
