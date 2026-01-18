# Roadmap

This roadmap tracks the development of GoForge.

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
- [ ] **Log Persistence:** Store logs in database/files for historical viewing (currently memory/stream only).
- [ ] **Alerting System:** Email/Slack notifications for failed deployments or high resource usage.
- [ ] **Status Page:** Public-facing status page for services.
- [ ] **Health Checks:** Add real comprehensive health checks for DB, Docker, and other dependencies.

### 2. Enhanced Services
- [ ] **Database Backups:** Automated nightly backups to S3/MinIO.
- [ ] **More Templates:** MySQL, MongoDB, MinIO, Plausible, PocketBase.
- [ ] **Service Networking:** Private internal networking between projects and services.
- [ ] **Lifecycle Management:** Implement actual restart logic and proper container stopping in service instances.

### 3. Team & Security
- [ ] **Team Management:** Invite users, Role-based Access Control (RBAC).
- [ ] **Audit Logs:** Track who did what and when (13.4).
- [ ] **2FA:** Two-factor authentication for local accounts.
- [ ] **Data Encryption:** Encrypt project variables marked as secret and OAuth access tokens at rest.

### 4. Developer Experience
- [ ] **CLI Tool:** `goforge` CLI for deploying from terminal.
- [ ] **API Tokens:** Personal Access Tokens for external integrations.
- [ ] **Build Caching:** S3/Local cache for faster Docker builds.
- [ ] **UX Polish:** Show rollback info via toast notifications and finalize login template rendering.

### 5. Mobile & Responsive Design
- [ ] **Navigation:** Implement hamburger menu and mobile-optimized sidebar.
- [ ] **Dashboard Cards:** Ensure stackable layouts for project/service stats on small screens.
- [ ] **Tables:** Add horizontal scroll or card view for deployments/containers lists on mobile.
- [ ] **Touch Targets:** Verify button and input sizes meet accessibility standards (min 44px).

## 🛠 Code Review Findings (Priority Fixes)
- [ ] **Pipeline Robustness:**
    - [ ] Integrate the unused `BlueGreenDeployer` into the main `RunPipeline` (currently dead code).
    - [ ] Implement a persistent deployment queue (Redis/Postgres) to survive restarts (currently in-memory).
    - [ ] Add graceful shutdown for deployment workers.
- [ ] **Service Management:**
    - [ ] Implement `StopInstance` and `RestartInstance` functionality (currently missing).
    - [ ] Fix orphaned containers: Ensure Docker containers are removed when a Service Instance is deleted.
- [ ] **Security & Error Handling:**
    - [ ] **Information Leakage:** Stop returning raw error strings to valid users in `auth.go` (e.g. `GitHubCallback`).
    - [ ] **Structured Logging:** Replace `fmt.Printf` with the structural logger in critical paths (`deploy/queue.go`, `services/instance.go`).
- [ ] **Configuration:**
    - [ ] Replace hardcoded `/tmp` paths in build pipeline with configured directories.

## 🔮 V2.0: Multi-Server (Future)

- [ ] **Remote Agents:** Deploy to multiple servers via SSH/Agent.
- [ ] **Cluster Management:** Simple orchestration across nodes.
- [ ] **Load Balancing:** Distributed traffic handling.