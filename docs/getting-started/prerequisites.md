# Prerequisites

Before installing GoForge, ensure your system meets the following requirements.

## Required Software

### Go 1.25+

GoForge requires Go 1.25 or later. Download from [go.dev](https://go.dev/dl/).

```bash
# Verify installation
go version
# Expected: go version go1.25.x linux/amd64
```

### Docker Engine

Docker is required for building and running application containers, as well as one-click services.

```bash
# Install Docker (Ubuntu/Debian)
curl -fsSL https://get.docker.com | sh

# Add your user to the docker group
sudo usermod -aG docker $USER

# Verify installation
docker version
```

### PostgreSQL

GoForge uses PostgreSQL as its primary database. You can run it locally or via Docker.

=== "Docker (Recommended)"

    ```bash
    docker run -d \
      --name goforge-db \
      -e POSTGRES_USER=goforge \
      -e POSTGRES_PASSWORD=password \
      -e POSTGRES_DB=goforge \
      -p 5432:5432 \
      postgres:15-alpine
    ```

=== "System Package"

    ```bash
    # Ubuntu/Debian
    sudo apt install postgresql postgresql-contrib
    
    # Create database and user
    sudo -u postgres createuser goforge
    sudo -u postgres createdb -O goforge goforge
    sudo -u postgres psql -c "ALTER USER goforge PASSWORD 'password';"
    ```

### templ CLI

The `templ` tool generates Go code from `.templ` template files.

```bash
go install github.com/a-h/templ/cmd/templ@v0.3.1001
```

## Optional Software

### TailwindCSS

Required for rebuilding the CSS. The development server and Docker build handle this automatically if installed.

```bash
# Install standalone CLI
curl -sLO https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-linux-x64
chmod +x tailwindcss-linux-x64
sudo mv tailwindcss-linux-x64 /usr/local/bin/tailwindcss
```

### air (Hot Reload)

For development with automatic hot reload on file changes.

```bash
go install github.com/air-verse/air@latest
```

### golangci-lint

For running the full linter suite locally.

```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

## System Requirements

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 1 core | 2+ cores |
| RAM | 1 GB | 4+ GB |
| Disk | 10 GB | 50+ GB |
| OS | Linux (amd64/arm64) | Ubuntu 22.04+ / Debian 12+ |

!!! note "Docker socket access"
    GoForge requires access to the Docker daemon socket (`/var/run/docker.sock`) to manage containers. The user running GoForge must be in the `docker` group or run as root.
