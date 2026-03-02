# Quick Start

Get GoForge running locally in under 5 minutes.

## 1. Clone the Repository

```bash
git clone https://github.com/raythurman2386/goforge.git
cd goforge
```

## 2. Configure Environment

```bash
cp .env.example .env
```

For development, the defaults work out of the box. The key setting is:

```bash
GOFORGE_DEV_MODE=true
```

When dev mode is enabled, GoForge automatically generates encryption keys, session secrets, and CSRF secrets. You only need to configure these manually for production.

## 3. Start PostgreSQL

If you do not already have PostgreSQL running:

```bash
docker run -d \
  --name goforge-db \
  -e POSTGRES_USER=goforge \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=goforge \
  -p 5432:5432 \
  postgres:15-alpine
```

## 4. Install Dependencies

```bash
go mod download
```

## 5. Run Database Migrations

```bash
make migrate
```

## 6. Start the Development Server

=== "With Hot Reload (Recommended)"

    ```bash
    make dev
    ```

    This uses `air` to watch for file changes and automatically rebuild. It also regenerates templ templates on change.

=== "Manual Build & Run"

    ```bash
    make build
    ./bin/goforge serve
    ```

## 7. Access GoForge

Open [http://localhost:8080](http://localhost:8080) in your browser.

On first visit, you will see the login page. Click **Register** to create your first account.

## Next Steps

- [Set up GitHub OAuth](../configuration/authentication.md) for Git integration
- [Deploy your first project](../guides/projects.md)
- [Add one-click services](../guides/services.md) like PostgreSQL or Redis
- [Configure for production](../deployment/index.md)

## Troubleshooting

### Port 8080 already in use

Change the port in your `.env` file:

```bash
GOFORGE_PORT=3000
```

### Database connection refused

Ensure PostgreSQL is running and the `DATABASE_URL` in `.env` matches your setup:

```bash
# Local PostgreSQL
DATABASE_URL=postgres://goforge:password@localhost:5432/goforge?sslmode=disable

# Docker PostgreSQL
DATABASE_URL=postgres://goforge:password@db:5432/goforge?sslmode=disable
```

### templ not found

Install the templ CLI:

```bash
go install github.com/a-h/templ/cmd/templ@v0.3.1001
```

### Permission denied on Docker socket

Add your user to the docker group:

```bash
sudo usermod -aG docker $USER
# Log out and back in for the change to take effect
```
