# Roadmap

This roadmap tracks the development of GoForge. Last brought up to date through #91 (2026-10-30).

## 🚨 CRITICAL: User Flow & Experience Gaps (Immediate Priority)

Based on comprehensive user flow analysis, the following must be addressed before V1.0 can be considered complete:

### Phase 1: Critical User Experience (Blocking V1.0)

#### 1. User Account Management (Completed Mar 2026)
- [x] **User Profile Page** (`/settings/profile`)
  - Update email and personal information
  - Change password functionality
  - View/manage connected GitHub account
  - Account deletion with confirmation flow
- [x] **API Token Management** 
  - Generate/revoke personal access tokens
  - Token permissions and expiration
  - Usage tracking

#### 2. First-Time User Experience (Completed Mar 2026)
- [x] **Onboarding Flow**
  - Welcome modal after registration (replaced by Dashboard step-by-step UI)
  - Step-by-step guide: Connect Git → Create Project → Deploy
  - Sample project template option (Deferred to Quick-start)
- [x] **Empty State Improvements**
  - Prompt to add Git Source when creating project (if none exist)
  - Guided tour for first-time project creation
  - Quick-start wizard for common stacks (Deferred to V1.1)

#### 3. Container & Runtime Management (Completed Mar 2026)
- [x] **Container Logs Viewer** (`/containers/{id}/logs`)
  - Real-time log streaming via SSE
  - Historical log search/filter
  - Download logs functionality
- [x] **Container Controls**
  - Stop/start/restart individual containers
  - Exec/shell access to running containers
  - Resource limit adjustment (CPU/Memory)
- [x] **Container List View**
  - Global view of all running containers
  - Filter by project/service status

#### 4. Domain & SSL Management
- [ ] **SSL Certificate Dashboard**
  - Certificate status and expiration dates
  - Force renew/refresh certificates
  - Domain validation status
- [ ] **Domain Configuration Validation**
  - DNS verification before adding custom domain
  - Error messages for misconfigured domains
  - Auto-generated subdomain preview

### Phase 2: UX Polish & Edge Cases

#### 5. Navigation & Wayfinding
- [x] **Breadcrumbs** (Add to all major pages) *(Done: `ui.Breadcrumb` (UI kit, replaces `components.Breadcrumbs` from #67) is used on project, service, deployment, and container detail)*
  - Project detail: Dashboard → Projects → {Project Name}
  - Service detail: Dashboard → Services → {Service Name}
  - Deployment detail: Already has breadcrumbs (reference implementation)
- [ ] **Back Navigation**
  - Consistent back buttons on detail pages
  - Preserve filter/sort state when returning to lists
- [x] **Active Sidebar State** *(#65)*
  - Highlight current page in sidebar (`data-nav` links; `static/js/app.js` sets `aria-current="page"` on the longest prefix match)
  - Show project list in sidebar: dropped. #65 removed the static "Recent projects" section instead of populating it

#### 6. Form & Validation Improvements
- [ ] **Client-Side Validation**
  - Validate required fields before submission
  - Real-time password strength indicator
  - Email format validation
- [ ] **Inline Error Messages**
  - Field-level error display (not just top-level)
  - Specific validation messages
- [ ] **Loading States** *(Partial, #65)*
  - [x] Disable buttons during HTMX requests (`hx-disabled-elt` on Deploy Now, service Restart and Update image, Sync from repo, and the HTMX form submits)
  - [x] Show spinner on async actions (`components.Spinner` via `hx-indicator`)
  - [ ] Progress indicators for long operations

#### 7. Mobile Responsiveness (High Priority)
- [ ] **Table Views**
  - [x] Horizontal scroll for deployment/service tables *(`ui.Table` scroll region, UI kit PR6)*
  - Card view option for mobile
  - Collapsible columns
- [ ] **Navigation** *(Partial)*
  - [x] Hamburger menu for mobile sidebar (`#mobile-menu-btn` opens the `ui.Sheet` navigation in `layouts/base.templ`; UI kit PR6)
  - [ ] Bottom tab bar for primary actions
  - [ ] Touch-friendly button sizing (min 44px) *(Partial, UI kit: every icon-only button (`ui.ButtonIcon`, Alert and toast dismiss) is 44 px below `md`; text buttons (`ButtonSM` about 32 px) and inputs (about 38 px) are smaller and not yet changed)*
- [ ] **Form Layouts**
  - Stack form fields on mobile
  - Full-width buttons on small screens

### Phase 3: Technical Debt & TODOs

#### 8. Health Checks
- [x] **Implement Real Health Checks** (Completed Mar 2026)
  - Database connectivity check (already implemented)
  - Docker daemon health check (already implemented)
  - Disk space validation (existing via Docker)
  - Blue-green health checks for deployments (newly integrated)

#### 9. Documentation & Help
- [ ] **Service Template Documentation**
  - Inline help for configuration fields
  - Connection string examples
  - Required vs optional field indicators
- [ ] **Contextual Help Tooltips**
  - Environment variable naming conventions
  - Build context explanations
  - Domain configuration guide

---

## ✅ V1.0 Release (Completed)

**Core Foundation & Deployment**
- [x] **Project Foundation:** Config, logger, router, Postgres DB, Migrations.
- [x] **Authentication:** GitHub OAuth & Local Auth (Argon2id).
- [x] **Git Integration:** GitHub, GitLab, Gitea, SSH Keys, Webhooks (Verified).
- [x] **Docker Engine:** Build pipeline, Container management, Docker Compose parsing.
- [x] **Traefik Proxy:** Automatic SSL (Let's Encrypt), Custom Domains, Label generation.

**Features & UX**
- [x] **Dashboard:** HTMX-driven UI, Project/Environment management.
- [x] **Deployments:** Blue-Green strategy, Rollbacks, Manual triggers.
- [x] **Monitoring:** Real-time stats (CPU/RAM) via SSE, Container logs.
- [x] **Services:** One-click catalog (Postgres, Redis), Template schema.
- [x] **System:** Rate limiting, Systemd service, Graceful shutdown.

---

## 🚀 V1.1: Resilience & Scale (Next Steps)

The focus for the next release is hardening the platform and improving the developer experience for power users.

### 1. Advanced Observability
- [x] **Log Persistence:** Clone and build logs are stored in `deployment_logs` and replayed when the deployment log stream connects.
- [ ] **Alerting System:** Email/Slack notifications for failed deployments or high resource usage.
- [ ] **Status Page:** Public-facing status page for services.
- [x] **Health Checks:** Add real comprehensive health checks for DB, Docker, and other dependencies. *(Moved to Critical Phase)*
- [ ] **Notifications Center:** In-app notification system for deployment events

### 1.5 Recent V1.0.x Improvements (Completed)
- [x] **Environment Variable Sync:** Added `.env.example` sync from repository
- [x] **Service Linking:** Automatic env var injection when linking services to projects
- [x] **Variable Management:** Delete and sync buttons for environment variables
- [x] **Shared Service Env Var Naming:** Extracted `ServiceEnvVarName` to avoid collisions
- [x] **Improved Secret Detection:** Segment-based matching to reduce false positives
- [x] **Service credential encryption:** One-click service `config` and `connection_info` are encrypted, and `generate: true` passwords are created on the server
- [x] **Admin role:** Accounts are users until `GOFORGE_ADMIN_EMAIL` grants admin. Host prune requires the admin role
- [x] **Deployment cancel status:** The `canceled` status matches the database check constraint

### 2. Enhanced Services
- [x] **Database Backups:** Nightly backups to S3-compatible storage (R2, B2, MinIO): GoForge database, Postgres services, Gitea, Vaultwarden. `goforge backup list` / `goforge backup restore` restore the GoForge database (side database, then rename; previous one kept), with a read-only Restore points list on Settings → Profile. Service data restore is manual. Not yet: other templates, service restore tooling, web/API restore, run-now from the API
- [x] **More Templates:** MySQL, MongoDB, MinIO *(shipped in `templates/`, alongside MariaDB, NocoDB, Uptime Kuma, Gitea, Vaultwarden, and SolidCraft)*
- [ ] **More Templates (next):** Plausible, PocketBase.
- [ ] **Service Networking:** Private internal networking between projects and services.
- [x] **Lifecycle Management:** Service instances stop, start, and restart their Docker container (`InstanceService.StopInstance` / `StartInstance` / `RestartInstance`; `POST /services/{id}/restart`), and delete removes the container.

### 3. Team & Security
- [ ] **Team Management:** Invite users, Role-based Access Control (RBAC). *(Moved to Critical Phase. The `admin`/`user` role and `GOFORGE_ADMIN_EMAIL` grant shipped in #28; teams, invites, and per-project roles have not.)*
- [ ] **Audit Logs:** Track who did what and when (13.4). *(Moved to Critical Phase - partially implemented. Every real `/api/v1` mutation writes an `api_v1.<action>` row first (#61); UI coverage is still partial)*
- [ ] **2FA:** Two-factor authentication for local accounts.
- [x] **Data Encryption:** Encrypt project variables marked as secret and OAuth access tokens at rest. *(AES-GCM with `ENCRYPTION_KEY`. Git source tokens and SSH keys and one-click service credentials are encrypted too. Legacy plaintext rows are encrypted on their next update.)*
- [ ] **API Key Management:** Secure storage and rotation of service API keys
- [x] **Security hardening (security queue Goals 1-3, #69-#71):** govulncheck at 0 reachable (#69); client IP from `TRUSTED_PROXIES` (rightmost untrusted hop, `CF-Connecting-IP` behind `CLOUDFLARE_PROXY_MODE`), login lockout, security headers, HMAC CSRF keyed by `CSRF_SECRET`, hourly expired-session purge (#70); repo Compose Traefik dashboard off and loopback-only, pinned `cloudflared` and `alpine`, Docker socket trust model in `SECURITY.md` (#71)

### 4. Developer Experience
- [ ] **CLI Tool:** `goforge` CLI for deploying from terminal.
- [x] **API Tokens:** Personal Access Tokens for external integrations, with `read` / `write` / `protected` scopes for `/api/v1` (#61).
- [x] **JSON API and MCP:** `/api/v1` (bearer only, dry-run by default, audited mutations) and the `goforge-mcp` stdio server (#61).
- [x] **MCP over HTTP:** `/mcp` on `goforge serve` with scoped Profile tokens (`gf_` prefix, hint, rotate, audited), a Profile MCP card with `mcp.json`, and the stdio binary kept as a fallback pointed at HTTPS. *(Not yet: OAuth, per-token rate limits, MCP ownership of Postgres tenants, Redis ACL users, and backups.)*
- [ ] **Build Caching:** S3/Local cache for faster Docker builds.
- [x] **UX Polish:** Finalize login template rendering.
- [ ] **Rollback feedback:** Show the rollback result in the UI. `RollbackDeployment` discards it today because `HX-Redirect` reloads the page and would drop a toast (`internal/web/handlers/deployments.go`).

### 5. Mobile & Responsive Design
- [x] **Navigation:** Hamburger menu and slide-over sidebar on small screens. *(Bottom tab bar is still open in the Critical Phase)*
- [x] **Dashboard Cards:** Ensure stackable layouts for project/service stats on small screens.
- [ ] **Tables:** Add horizontal scroll or card view for deployments/containers lists on mobile. *(Moved to Critical Phase. Partial: `ui.Table` wraps every table in a keyboard-scrollable region; no card view)*
- [ ] **Touch Targets:** Verify button and input sizes meet accessibility standards (min 44px). *(Moved to Critical Phase. Partial: icon-only buttons meet it below `md`)*

## 🛠 Code Review Findings (Priority Fixes)

### Recent Fixes (V1.0.x)
- [x] **Environment Variable System:**
    - [x] Extracted `ServiceEnvVarName` to shared `internal/services/envvars.go` package
    - [x] Fixed collision issue: Now uses instance name prefix (e.g., `MYDB_DATABASE_URL`)
    - [x] Improved secret detection with segment-based matching (avoids false positives like KEYWORD)
    - [x] Added idempotency checks in `CreateProject` env var seeding
    - [x] Added `SeedEnvVarsFromRepo` method for manual sync
    - [x] Added delete button for environment variables
    - [x] Fixed `.env.example` parsing to handle quoted values and inline comments

- [x] **Pipeline Robustness (Mar 2026):**
    - [x] **Integrated BlueGreenDeployer** into the main `RunPipeline` for zero-downtime deployments with health checks
    - [x] Added graceful period (5 minutes) for blue-green deployments before cleanup
    - [x] Extended cleanup timeout to 10 minutes to accommodate grace period
    - [x] Added container inspection for health check IP detection

- [x] **Security & Error Handling (Mar 2026):**
    - [x] **Fixed Information Leakage:** Replaced raw `http.Error` strings with proper `RenderError` in auth.go
    - [x] Fixed dead code: Removed duplicate `Health`, `HealthReady`, and `LoginPage` functions from handlers.go

- [x] **Container Management UI (Mar 2026):**
    - [x] Added global containers view (`GET /containers`)
    - [x] Added container detail page with live logs viewer
    - [x] Added container stop/restart controls via HTMX
    - [x] Added container type detection (Deployment/Service/External)
    - [x] Integrated with existing SSE endpoints for real-time logs

### Outstanding Issues (all items below are done)
- [x] **Pipeline Robustness:**
    - [x] Implement a persistent deployment queue (PostgreSQL-backed) to survive restarts.
    - [x] Add graceful shutdown for deployment workers.
- [x] **Service Management:**
    - [x] Implement `StopInstance` and `RestartInstance` functionality (currently missing).
    - [x] Fix orphaned containers: Ensure Docker containers are removed when a Service Instance is deleted.
- [x] **Security & Error Handling:**
    - [x] **Information Leakage:** Fixed - replaced raw error strings with proper HTML error pages in auth.go
    - [x] **Structured Logging:** Replace `fmt.Printf` with the structural logger in critical paths (`deploy/queue.go`, `services/instance.go`).
- [x] **Configuration:**
    - [x] Replace hardcoded `/tmp` paths in build pipeline with configured directories.

## 📊 V1.0 Release Readiness Checklist

### Definition of Done for V1.0 GA
Before tagging V1.0, the following **must** be completed:

**Critical User Flows (All Working End-to-End):**
- [x] User can register/login via email or GitHub *(registration closes after the first account unless `GOFORGE_ENABLE_REGISTRATION=true`)*
- [x] Operator gets the admin role through `GOFORGE_ADMIN_EMAIL` (case-insensitive); everyone else is `user` (#28)
- [x] Another account's project or deployment returns `404` (#28)
- [x] User can create a project with Git repository
- [x] User can deploy the project successfully
- [x] User can view deployment logs in real-time
- [x] User can link services and see connection strings injected
- [x] **User can view container logs (completed Mar 2026)**
- [x] **User can restart/stop containers (completed Mar 2026)**
- [x] **User can update their profile/password (completed Mar 2026)**
- [x] **User can add/manage API tokens (completed Mar 2026)**

**Documentation:**
- [x] API documentation for all endpoints *(`docs/reference/api-routes.md` for UI routes, `docs/reference/api-v1.md` for the JSON API; #61, #64)*
- [ ] User guide for first-time setup *(README now has a Compose quickstart)*
- [ ] Deployment troubleshooting guide
- [ ] Environment variable naming conventions doc

**Quality Assurance:**
- [ ] All critical user paths tested manually
- [ ] Mobile responsiveness verified on actual devices
- [ ] Accessibility audit (WCAG 2.1 AA) *(the `drive-ui-a11y` axe scan, WCAG 2.2 AA tags, reports 0 serious or critical issues on its 32 light and dark scans since `fix/axe-leftovers-and-docs`; a manual audit, including screen readers, is still open)*
- [ ] Security review of auth flows *(2026-10-29 audit findings on client IP, lockout, CSRF, and session purge fixed in #70; a dedicated auth-flow review is still open)*
- [ ] Load testing for concurrent deployments

---

## 📦 Shipped since V1.0.x (#58-#91)

- [x] **#58** Redis template enforces `requirepass`; services without a Domain get `traefik.enable=false`; Postgres health check and MinIO `command` fixed
- [x] **#59** A1 shared data services: Postgres 17 (`pgadmin`), Redis 7.4 (AOF, `noeviction`), `scripts/shared-data/pg-tenant.sh`
- [x] **#60** Redis per-app ACL users: default password in `/data/users.acl` (no `requirepass`), `scripts/shared-data/redis-tenant.sh` (`~<app>:*`); no live tenants provisioned
- [x] **#61** Scoped JSON API `/api/v1` and the `goforge-mcp` stdio server
- [x] **#62** Nightly S3-compatible backups (default 02:00 America/Chicago, 7 daily kept); backup only
- [x] **#63** `TestInstanceSource` flake fixed
- [x] **#64** `CONTRIBUTING.md` rewrite and docs drift after #61 and #62
- [x] **#65** UI shell: logout, toasts and HTMX error toasts, containers list target, active sidebar, button spinners (aesthetics Goal 1)
- [x] **#66** `/containers/api` no longer shadowed by `/containers/{id}`
- [x] **#67** Design system: `.card` / `.btn-*`, `StatusBadge`, `PageHeader`, `Breadcrumbs`, accessible `Modal`, self-hosted Inter and favicon (aesthetics Goal 2)
- [x] **#68** System dark mode (`prefers-color-scheme`, no toggle), content-hashed `output.css`, CI `css` job with `scripts/class_check.py` (aesthetics Goal 3)
- [x] **#69** Dependency and toolchain bump to 0 reachable govulncheck findings; Docker SDK moved to `github.com/moby/moby/client` + `api`; `golang:1.26.10` builder (security Goal 1)
- [x] **#70** Client IP / XFF spoofing fix, login lockout, security headers, HMAC CSRF, expired-session purge (security Goal 2)
- [x] **#71** Repo Compose hardening (Traefik dashboard off, loopback port, pinned images) and Docker socket trust model (security Goal 3)
- [x] **#72** Cleanup and roadmap truth (security Goal 4): this ROADMAP and `CHANGELOG.md` through #71, `.env.example` gaps, Tailwind v3.4.17 pin in `make css`, `css-check` in `make ci`, golangci-lint pinned in CI, stale docs
- [x] **#73** Sidebar Log out stays on screen; dashboard skeleton mirrors the stats
- [x] **#74** Redis per-app ACL users (#60 template change)
- [x] **#75** Service edits claim the instance with a status compare-and-swap; a concurrent edit gets `409` (#48)
- [x] **#76** Aesthetics Goal 3 nits from #68
- [x] **#77** Backup settings in Settings → Profile (S3 keys sealed with `ENCRYPTION_KEY`)
- [x] **#78** MCP over HTTP at `/mcp` with Profile API tokens (create, rotate, revoke)
- [x] **#79** `goforge backup list` and `goforge backup restore` for the GoForge database (CLI)
- [x] **#80** Repo Compose stops passing `BACKUP_*` into the `goforge` container (ENV-CREDS)
- [x] **#81** Stats stream resumes after a config save; Container card on service detail and project Overview
- [x] **#82-#87** UI kit (Path A): tokens, `components/ui` (20 components), native `<dialog>` and `popover`, `static/js/ui.js`, `/dev/ui` gallery behind `GOFORGE_DEV_MODE`, `drive-ui-a11y` (axe plus keyboard scripts, light and dark); every page migrated
- [x] **#88** Script URLs are cache-busted so the mobile navigation opens after a deploy
- [x] **#89** Final sweep: last inline handlers and legacy classes removed, `docs/reference/ui-kit.md` added
- [x] **#90** Body scroll locks while the mobile Sheet is open
- [x] **#91** Primary buttons rest on `primary-700` and primary text uses `text-primary-700`, for color contrast
- [x] **`fix/axe-leftovers-and-docs`** `.btn-warning`, muted gray text, and the Overview `dlitem` fixed, so `drive-ui-a11y` reports 0 serious or critical issues; security docs and README brought up to date

## 🅿️ Parked and follow-up

Parked items need Raymond's OK before anyone starts them.

- [x] **Backup restore (GoForge database)**: `goforge backup list` and `goforge backup restore <run key>` (CLI only), read-only Restore points on Settings → Profile; PostgreSQL 15 runbook fixed (`docs/deployment/backup.md`)
- [ ] **Backup restore, follow-up** (parked): service data restore (Postgres services, Gitea, Vaultwarden), web/API/MCP restore, run-now from the API
- [x] **Aesthetics Goal 3 nits** (#68 leftovers): `class_check.py` also checks `class={ ... }`, `*Class()` helpers, and `app.js` classes; `make css` fails unless it runs Tailwind v3.4.17 (standalone or `npx`); unused `.sidebar-link*` removed; dark `select` chevron; auth-aware error page (B17)
- [ ] **Deadcode sweep** (parked): `deadcode ./...` reported 96 unreachable functions on 2026-10-29. Not removed in the cleanup goal; needs a reviewed pass
- [ ] **Stale remote branches** (needs Raymond's OK): about 30 squash-merged branches remain on `origin` (for example `ui/shell-fixes`, `security/auth-edge-hardening`, `fix/4x-*`). Delete only with approval
- [ ] **Open security findings**, documented in `SECURITY.md`: `/health/ready` is public and returns error text (HEALTH-READY-PUBLIC); main image runs as root. Empty `WEBHOOK_SECRET` outside dev mode is closed: GoForge refuses to start (#92). ENV-CREDS is closed for the repo Compose file (#80)
- [ ] **Rollback feedback** in the UI (see Developer Experience)

---

## 🔮 V2.0: Multi-Server (Future)

- [ ] **Remote Agents:** Deploy to multiple servers via SSH/Agent.
- [ ] **Cluster Management:** Simple orchestration across nodes.
- [ ] **Load Balancing:** Distributed traffic handling.