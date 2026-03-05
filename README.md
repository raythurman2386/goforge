# GoForge

> **⚠️ Security Warning: This project is in active development.** 
> 
> GoForge is a self-hosted PaaS that runs containerized workloads on your infrastructure. 
> Like any system that handles authentication, secrets, and container execution, it requires 
> careful security considerations before production use. See [Security Considerations](#security-considerations) below.

---

GoForge is a lightweight, self-hosted Platform as a Service (PaaS) built in Go.

## Features

- **Authentication:** Local email/password and GitHub OAuth.
- **Project Management:** CRUD operations for projects and environments.
- **Docker Integration:** Build and deploy containers directly from git repositories.
- **Traefik Integration:** Automatic routing with Docker labels (internal only).
- **Cloudflare Tunnel:** Secure public access without opening inbound ports.
- **Monitoring:** Real-time container resource monitoring (CPU, Memory).
- **Real-time Updates:** SSE-powered deployment logs and status updates.

## Getting Started

### Prerequisites

- Go 1.25+
- Docker Engine
- PostgreSQL
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

## Production Deployment

To run GoForge in production using Systemd:

1.  **Build the binary:**
    ```bash
    make build
    ```

2.  **Prepare the directory:**
    ```bash
    sudo mkdir -p /opt/goforge
    sudo cp bin/goforge /opt/goforge/
    sudo cp -r static /opt/goforge/
    sudo cp .env.example /opt/goforge/.env
    # Edit /opt/goforge/.env with your production secrets
    ```

3.  **Install the Service:**
    ```bash
    sudo cp deploy/goforge.service /etc/systemd/system/
    sudo systemctl daemon-reload
    sudo systemctl enable --now goforge
    ```

## Architecture

GoForge follows a clean architecture with:
- **Cmd:** Entry point and CLI commands.
- **Internal:** Core logic separated into `auth`, `project`, `docker`, `git`, `deploy`, `proxy`, `sse`, and `monitor`.
- **Web:** HTMX + Templ frontend with a RESTful-ish backend.
- **Database:** Repository pattern supporting PostgreSQL.

## Security Considerations

### ⚠️ Important Security Notes

1. **Not Production-Ready for Untrusted Environments**
   - GoForge is designed for teams/individuals hosting their own applications
   - Do not expose to untrusted users without proper review
   - Review and test thoroughly before any production deployment

2. **Secret Management**
   - Never commit `.env` files or secrets to version control
   - Use strong, unique secrets for session keys and encryption
   - Rotate credentials regularly
   - Consider using Docker secrets or external secret stores

3. **Network Security**
   - GoForge is designed to work with **Cloudflare Tunnel** (recommended)
   - No inbound ports needed - tunnel connects outbound to Cloudflare
   - Restrict access to the Docker socket
   - Use TLS/SSL for all external connections (handled by Cloudflare)
   - Configure firewall to allow only outbound connections to Cloudflare IPs

4. **Container Security**
   - Review deployed container images for vulnerabilities
   - Avoid running containers as root where possible
   - Implement resource limits to prevent resource exhaustion
   - Regularly update base images and dependencies

5. **Authentication & Authorization**
   - Use strong passwords for local accounts
   - Review OAuth permissions requested by GitHub integration
   - Consider implementing 2FA (not yet implemented)
   - Audit logs are partially implemented - complete before production use

6. **Data Protection**
   - Regular backups of PostgreSQL database
   - Encrypt sensitive data at rest (encryption planned for future)
   - Secure connection strings for linked services

### Reporting Security Issues

If you discover a security vulnerability, please follow our [Security Policy](SECURITY.md).

---

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.
