# Contributing to GoForge

## 1. What GoForge is

GoForge is a lightweight, self-hosted PaaS written in Go with an HTMX and templ
UI. It deploys apps from Git, runs one-click services, routes through Traefik,
and backs itself up to S3-compatible storage.

Where to start:

- **Humans** read this file.
- **AI agents** start at [AGENTS.md](AGENTS.md). It holds the code style,
  commands, and security rules this file links to instead of copying.
- **Users and operators** read the docs site sources in [`docs/`](docs/), built
  with MkDocs (`mkdocs.yml`).

## 2. Get it running

The supported way to run GoForge is Docker Compose. Follow the
[README quickstart](README.md#quickstart-with-docker-compose).

To work on the code:

```bash
cp .env.example .env     # fill in DATABASE_URL at least
go mod download          # templ and golangci-lint run through `go tool`
make install-tools       # installs air for hot reload
make migrate             # applies migrations to DATABASE_URL
make dev                 # http://localhost:8080 with GOFORGE_DEV_MODE=true
```

You need Go 1.26 or newer, Docker Engine, a PostgreSQL you can reach (CI uses
16, the Compose stack uses 15), and the Tailwind CSS v3 standalone CLI for
`make css`. Full setup: [docs/development/setup.md](docs/development/setup.md).

`make dev` turns on dev mode for that one process. Never copy a dev-mode
environment to a server.

## 3. How the code is laid out

```
cmd/goforge/          # Server and CLI: serve, migrate, backup run
cmd/goforge-mcp/      # MCP server over stdio; calls /api/v1 with a scoped token
internal/
  apiv1/              # JSON API /api/v1: bearer tokens, scopes, gated writes
  auth/               # Sessions, local and GitHub login, CSRF
  backup/             # Nightly S3 backups and `goforge backup run`
  config/             # Environment config and validation
  database/           # Connection, migrations, repositories
  deploy/             # Deployment pipeline and queue
  docker/             # Docker SDK wrapper
  git/                # GitHub, GitLab, Gitea providers
  logger/             # Structured logging
  mcpserver/          # MCP tools used by cmd/goforge-mcp
  models/             # Domain types
  monitor/            # Container metrics
  pagination/         # Pagination helpers
  project/            # Projects and environments
  proxy/              # Traefik labels
  secrets/            # AES-GCM encryption at rest
  services/           # One-click service catalog and instances
  sse/                # Server-Sent Events hub
  web/                # Router, middleware, handlers, templ templates
  webhook/            # Git webhook handling
templates/            # One-click service YAML templates
docs/                 # MkDocs site
```

Handlers stay thin. They parse input, call a service, and render. Business
logic lives in services; SQL lives in repositories. More detail:
[docs/architecture/project-structure.md](docs/architecture/project-structure.md).

## 4. House rules

Each rule names what enforces it. If a rule has no automatic check, the
reviewer is the check.

1. **`make check` is green before you push.** It runs `fmt-check`, `vet`,
   `lint` (golangci-lint via `go tool`, pinned in `go.mod`), and `test` with
   the race detector. CI (`.github/workflows/ci.yml`) runs the same checks on
   every PR, but its lint step uses `golangci/golangci-lint-action@v6` with
   `version: latest`, not the `go.mod` pin, so CI can flag lints a local
   `make lint` does not.
2. **Follow the Go style in [AGENTS.md](AGENTS.md).** Wrapped errors,
   `context.Context` first, grouped imports, sentinel errors with
   `errors.Is`. Enforced by golangci-lint and review.
3. **Go never drops below the latest release.** `go.mod` says `go 1.26.0`
   today. Enforced by review.
4. **Secrets stay secret.** Never log a secret, token, or password. Never
   render one into a service template's `docker.command`. Never commit
   `.env`. Values encrypted with `ENCRYPTION_KEY` (secret env vars, OAuth and
   Git tokens, SSH keys, service `config` and `connection_info`) stay
   encrypted at rest. Enforced by `internal/secrets` tests, gosec, and
   review.
5. **Containers stay off the public network unless asked.** Services without a
   Domain get `traefik.enable=false`. Published ports bind to loopback only;
   `0.0.0.0` and `::` are refused. Templates set resource limits. Enforced by
   `internal/services` and `internal/docker` tests and review.
6. **Ownership answers `404`, not `403`.** A resource owned by another
   account looks like it does not exist. UI
   mutations need a CSRF token; `/api/v1` is bearer only and ignores cookies.
   Enforced by handler and `internal/apiv1` tests.
7. **API v1 and MCP writes stay gated.** Mutations are dry runs unless
   `"dry_run": false`, a real run needs `approval_ref`, and the audit row is
   written before anything changes. A failed audit insert blocks the change.
   Protected services need the `protected` scope. There is no route or tool
   that reveals a secret. Enforced by `internal/apiv1` and
   `internal/mcpserver` tests. See
   [docs/reference/api-v1.md](docs/reference/api-v1.md).
8. **Generated files are never committed.** `*_templ.go` and
   `static/css/output.css` are gitignored; regenerate them with `make templ`
   and `make css`.

## 5. Tests

| What | Where | Run |
|------|-------|-----|
| Unit tests | `*_test.go` next to the code | `make test` (CI) or `make test-short` |
| One test | any package | `go test -v -run TestName ./internal/pkg/...` |
| Integration tests | files tagged `//go:build integration` in `internal/backup` and `internal/services` | `go test -tags integration -run <Name> ./internal/<pkg>/` |
| UI journeys | `.cursor/skills/verify-goforge/` | `verify.sh launch`, then the feature file |
| Docs | `docs/`, `mkdocs.yml` | `mkdocs build --strict` |

Rules:

- A bug fix ships with a test that fails without the fix.
- Tests never call external services (GitHub, Cloudflare, real S3). Use fakes,
  or throwaway containers for integration tests.
- Integration tests create their own containers, networks, and volumes with a
  test prefix and remove them afterwards. Never point them at a production
  database, bucket, or Docker host that runs real services.
- A failing test is fixed in the same PR, not skipped to get green.

Details: [docs/development/testing.md](docs/development/testing.md).

## 6. Database migrations

Migrations live in `internal/database/migrations/postgres/` as numbered
`NNN_name.up.sql` and `NNN_name.down.sql` pairs. Create one with
`make migrate-create name=xyz`.

- Append only. Never edit a migration that has been merged; add a new one.
- Every `up` has a working `down`.
- Existing rows must keep working. Give new columns a default and backfill in
  the same migration (see `018_api_token_scopes`).
- Update [docs/reference/database-schema.md](docs/reference/database-schema.md)
  in the same PR.

## 7. Service templates

One-click services are YAML files in `templates/`. The schema is in
[docs/reference/service-templates.md](docs/reference/service-templates.md).

- A changed default applies to new instances only. Existing instances change
  on their next recreate (config edit or image update), so say so in the
  CHANGELOG.
- Values a service writes into its own data on first start (admin passwords,
  encryption keys, database names) are `immutable`.
- Keep secrets in `environment`, never in `docker.command`.
- Update `docs/reference/service-templates.md` and
  `docs/guides/services.md` in the same PR.

## 8. Docs

A change that users or operators can see updates the docs in the same PR:

- `CHANGELOG.md` under `## [Unreleased]`, with the issue or PR number.
- The page in `docs/` that describes the behavior.
- New environment variables in `.env.example` and
  `docs/configuration/index.md`.

Write plain sentences. Run `mkdocs build --strict` when you touch `docs/` or
`mkdocs.yml`; the docs workflow runs it on every PR that does.

```bash
pip install mkdocs-material mkdocs-minify-plugin
mkdocs build --strict
```

## 9. Sending a change

1. Branch from `main`. Name the branch after the change type: `feat/…`,
   `fix/…`, `docs/…`, `test/…`, `chore/…`.
2. Keep one topic per PR. Split unrelated fixes into their own PRs.
3. Write [Conventional Commits](https://www.conventionalcommits.org/):
   `type(scope): description`, for example
   `fix(services): refuse 0.0.0.0 host ports`.
4. Run `make check`, plus `mkdocs build --strict` for docs changes.
5. Open the PR against `main` and fill in the template:
   **What changed**, **Tests**, **Proof**, **Deferred**. Reference the issue
   (`Fixes #123`).
6. PRs are squash merged.

### Proof

Say what you ran and what you saw. For behavior visible in the UI or over
HTTP, include a verify-goforge drive, a `curl` transcript, or screenshots.
Keep proofs out of the repo: paste them in the PR or link them. `artifacts/`
is gitignored for this reason.

Do not push to a PR only to catch up with `main`. Rebase when there is a
conflict or the reviewer asks.

## 10. Bugs and security issues

- Bugs and feature requests: open an issue with the forms in
  `.github/ISSUE_TEMPLATE/`.
- Vulnerabilities: do not open a public issue. Follow
  [SECURITY.md](SECURITY.md).

## 11. License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
