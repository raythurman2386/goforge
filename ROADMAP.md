# Roadmap

This roadmap tracks the development of GoForge.

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
- [ ] **Breadcrumbs** (Add to all major pages)
  - Project detail: Dashboard → Projects → {Project Name}
  - Service detail: Dashboard → Services → {Service Name}
  - Deployment detail: Already has breadcrumbs (reference implementation)
- [ ] **Back Navigation**
  - Consistent back buttons on detail pages
  - Preserve filter/sort state when returning to lists
- [ ] **Active Sidebar State**
  - Highlight current page in sidebar
  - Show project list in sidebar (currently static)

#### 6. Form & Validation Improvements
- [ ] **Client-Side Validation**
  - Validate required fields before submission
  - Real-time password strength indicator
  - Email format validation
- [ ] **Inline Error Messages**
  - Field-level error display (not just top-level)
  - Specific validation messages
- [ ] **Loading States**
  - Disable buttons during HTMX requests
  - Show spinner on async actions
  - Progress indicators for long operations

#### 7. Mobile Responsiveness (High Priority)
- [ ] **Table Views**
  - Horizontal scroll for deployment/service tables
  - Card view option for mobile
  - Collapsible columns
- [ ] **Navigation**
  - Hamburger menu for mobile sidebar
  - Bottom tab bar for primary actions
  - Touch-friendly button sizing (min 44px)
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
- [x] **Admin role:** The first account is an admin. Later accounts are users. Host prune requires the admin role
- [x] **Deployment cancel status:** The `canceled` status matches the database check constraint

### 2. Enhanced Services
- [ ] **Database Backups:** Automated nightly backups to S3/MinIO.
- [ ] **More Templates:** MySQL, MongoDB, MinIO, Plausible, PocketBase.
- [ ] **Service Networking:** Private internal networking between projects and services.
- [ ] **Lifecycle Management:** Implement actual restart logic and proper container stopping in service instances.

### 3. Team & Security
- [ ] **Team Management:** Invite users, Role-based Access Control (RBAC). *(Moved to Critical Phase)*
- [ ] **Audit Logs:** Track who did what and when (13.4). *(Moved to Critical Phase - partially implemented)*
- [ ] **2FA:** Two-factor authentication for local accounts.
- [ ] **Data Encryption:** Encrypt project variables marked as secret and OAuth access tokens at rest.
- [ ] **API Key Management:** Secure storage and rotation of service API keys

### 4. Developer Experience
- [ ] **CLI Tool:** `goforge` CLI for deploying from terminal.
- [ ] **API Tokens:** Personal Access Tokens for external integrations. *(Moved to Critical Phase)*
- [ ] **Build Caching:** S3/Local cache for faster Docker builds.
- [x] **UX Polish:** Show rollback info via toast notifications and finalize login template rendering. *(Completed)*

### 5. Mobile & Responsive Design
- [ ] **Navigation:** Implement hamburger menu and mobile-optimized sidebar. *(Moved to Critical Phase)*
- [x] **Dashboard Cards:** Ensure stackable layouts for project/service stats on small screens.
- [ ] **Tables:** Add horizontal scroll or card view for deployments/containers lists on mobile. *(Moved to Critical Phase)*
- [ ] **Touch Targets:** Verify button and input sizes meet accessibility standards (min 44px). *(Moved to Critical Phase)*

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

### Outstanding Issues
- [ ] **Pipeline Robustness:**
    - [x] Implement a persistent deployment queue (PostgreSQL-backed) to survive restarts.
    - [x] Add graceful shutdown for deployment workers.
- [ ] **Service Management:**
    - [x] Implement `StopInstance` and `RestartInstance` functionality (currently missing).
    - [x] Fix orphaned containers: Ensure Docker containers are removed when a Service Instance is deleted.
- [ ] **Security & Error Handling:**
    - [x] **Information Leakage:** Fixed - replaced raw error strings with proper HTML error pages in auth.go
    - [x] **Structured Logging:** Replace `fmt.Printf` with the structural logger in critical paths (`deploy/queue.go`, `services/instance.go`).
- [ ] **Configuration:**
    - [x] Replace hardcoded `/tmp` paths in build pipeline with configured directories.

## 📊 V1.0 Release Readiness Checklist

### Definition of Done for V1.0 GA
Before tagging V1.0, the following **must** be completed:

**Critical User Flows (All Working End-to-End):**
- [x] User can register/login via email or GitHub
- [x] User can create a project with Git repository
- [x] User can deploy the project successfully
- [x] User can view deployment logs in real-time
- [x] User can link services and see connection strings injected
- [x] **User can view container logs (completed Mar 2026)**
- [x] **User can restart/stop containers (completed Mar 2026)**
- [x] **User can update their profile/password (completed Mar 2026)**
- [x] **User can add/manage API tokens (completed Mar 2026)**

**Documentation:**
- [ ] API documentation for all endpoints
- [ ] User guide for first-time setup
- [ ] Deployment troubleshooting guide
- [ ] Environment variable naming conventions doc

**Quality Assurance:**
- [ ] All critical user paths tested manually
- [ ] Mobile responsiveness verified on actual devices
- [ ] Accessibility audit (WCAG 2.1 AA)
- [ ] Security review of auth flows
- [ ] Load testing for concurrent deployments

---

## 🔮 V2.0: Multi-Server (Future)

- [ ] **Remote Agents:** Deploy to multiple servers via SSH/Agent.
- [ ] **Cluster Management:** Simple orchestration across nodes.
- [ ] **Load Balancing:** Distributed traffic handling.