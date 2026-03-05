# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial project setup
- GitHub CI workflow with fmt, vet, lint, and test checks
- Project documentation (README, CONTRIBUTING, LICENSE)
- `GetFileContent` implementation for Gitea and GitLab git providers
- Service restart handler (`POST /services/{id}/restart`)
- Real-time container resource monitoring (stats) to standard Git Projects

### Changed
- Renamed project to GoForge
- Simplified `doRequest` in Gitea and GitLab clients (removed redundant URL construction)
- Excluded gosec G115 (integer overflow) globally in `.golangci.yml` instead of per-line nolint directives
- Updated README.md to remove false claims of SQLite support
- Added UI toast feedback for "Prune System" action in settings
- Added `/goforge` build artifact to `.gitignore`

### Deprecated

### Removed
- Removed orphaned `CustomDomain` model and `internal/domain` logic that had no database repository

### Fixed
- SSE streaming connections (deployment logs, container stats/logs) killed after 15 seconds by global `WriteTimeout`; replaced with per-route timeout middleware so SSE routes stream indefinitely
- Stale documentation references to old project name "GoCool"/"gocool"
- Outdated security warnings in database schema docs (encryption is now implemented)
- `X-Forwarded-For` header trusted unconditionally allowing IP spoofing in audit logs and rate limiter
- Nil pointer panic in project detail tabs when a project has no environments
- `GetProject` handler silently swallowed non-`sql.ErrNoRows` deployment fetch errors
- Redundant `//nolint:errcheck` directive and misleading comment in settings handler
- CSS spacing inconsistency (`mt-6` inside grid with `gap-6`) on resource usage card

### Security
- Added trusted proxy validation for `X-Forwarded-For` / `X-Real-IP` headers (configurable via `TRUSTED_PROXIES` env var)

---

## [0.1.0] - 2026-01-11

### Added
- Core project structure
- Database migrations and PostgreSQL support
- GitHub OAuth authentication
- Project and environment management
- Git integration (GitHub, GitLab, Gitea)
- Docker build and deployment pipeline
- Traefik reverse proxy with Let's Encrypt
- Real-time logs via Server-Sent Events
- Resource monitoring (CPU, memory, disk)
- One-click service catalog
- HTMX-powered responsive dashboard
