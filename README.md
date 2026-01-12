# GoCool

A lightweight, self-hosted PaaS built in Go with HTMX. Deploy applications from Git with automatic SSL, monitoring, and one-click services.

[![CI](https://github.com/user/gocool/actions/workflows/ci.yml/badge.svg)](https://github.com/user/gocool/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/user/gocool)](https://goreportcard.com/report/github.com/user/gocool)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Overview

GoCool reimplements the core developer experience of [Coolify](https://coolify.io) — git-to-deploy workflows, one-click services, automatic SSL — while being:

- **Lightweight**: Single binary, minimal resource footprint
- **Fast**: Native Go + server-rendered HTMX (no SPA overhead)
- **Simple**: Hypermedia-driven architecture, easy to understand and contribute to

## Features

- **Git-based deployments** — GitHub, GitLab, Gitea, or raw SSH
- **Docker & Docker Compose** — Build and deploy containerized applications
- **One-click services** — PostgreSQL, Redis, MySQL, MongoDB, Plausible, PocketBase, and more
- **Automatic SSL** — Let's Encrypt certificates via Traefik
- **Real-time logs** — Deployment and container logs via Server-Sent Events
- **Resource monitoring** — CPU, memory, and disk usage per container
- **Environment management** — Dev, staging, production with separate configs

## Requirements

- Go 1.22+
- Docker Engine 24+
- PostgreSQL 15+
- Traefik (for reverse proxy and SSL)

## Quick Start

### 1. Clone and build

```bash
git clone https://github.com/user/gocool.git
cd gocool
make build
```

### 2. Configure environment

```bash
cp .env.example .env
# Edit .env with your configuration
```

### 3. Run database migrations

```bash
./bin/gocool migrate up
```

### 4. Start the server

```bash
./bin/gocool serve
```

Visit `http://localhost:8080` and sign in with GitHub.

## Development

### Prerequisites

```bash
# Required tools
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/air-verse/air@latest

# Download TailwindCSS standalone CLI
curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64
chmod +x tailwindcss-linux-x64
mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss
```

### Running locally

```bash
# Start PostgreSQL (if using Docker)
docker run -d --name gocool-db \
  -e POSTGRES_USER=gocool \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=gocool \
  -p 5432:5432 \
  postgres:16

# Run with hot reload
make dev
```

### Available commands

```bash
make build      # Build production binary
make dev        # Run with hot reload (air)
make test       # Run tests
make lint       # Run linters
make fmt        # Format code
make css        # Compile TailwindCSS
make templ      # Generate templ files
make migrate    # Run database migrations
make clean      # Clean build artifacts
```

## Configuration

GoCool is configured via environment variables. See [.env.example](.env.example) for all options.

| Variable | Description | Default |
|----------|-------------|---------|
| `DATABASE_URL` | PostgreSQL connection string | required |
| `ENCRYPTION_KEY` | 32-byte key for encrypting secrets (base64) | required |
| `SESSION_SECRET` | Secret for session cookies | required |
| `GITHUB_CLIENT_ID` | GitHub OAuth app client ID | required |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth app client secret | required |
| `GOCOOL_PORT` | HTTP server port | `8080` |
| `TRAEFIK_NETWORK` | Docker network for Traefik | `traefik` |
| `ACME_EMAIL` | Email for Let's Encrypt | required for SSL |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                     GoCool Server                       │
├─────────────────────────────────────────────────────────┤
│  HTTP/HTMX Handlers  │  SSE Hub  │  Background Workers  │
├─────────────────────────────────────────────────────────┤
│  Project │ Git │ Deploy │ Service Catalog │ Monitoring  │
├─────────────────────────────────────────────────────────┤
│         Docker Client  │  Traefik  │  PostgreSQL        │
└─────────────────────────────────────────────────────────┘
```

**Tech Stack:**
- **Backend**: Go, chi router
- **Frontend**: HTMX 2, templ, TailwindCSS
- **Database**: PostgreSQL
- **Proxy**: Traefik with Let's Encrypt
- **Runtime**: Docker

## Deployment

### Docker Compose (recommended)

```bash
cd deploy
docker compose up -d
```

See [deploy/docker-compose.yml](deploy/docker-compose.yml) for the full configuration.

### Systemd

```bash
sudo cp deploy/gocool.service /etc/systemd/system/
sudo systemctl enable gocool
sudo systemctl start gocool
```

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

```bash
# Run all checks before submitting
make fmt
make lint
make test
```

## License

MIT License. See [LICENSE](LICENSE) for details.

## Acknowledgments

- [Coolify](https://coolify.io) — The inspiration for this project
- [HTMX](https://htmx.org) — Hypermedia approach to modern web apps
- [templ](https://templ.guide) — Type-safe HTML templating for Go
