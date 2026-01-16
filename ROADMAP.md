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

### 2. Enhanced Services
- [ ] **Database Backups:** Automated nightly backups to S3/MinIO.
- [ ] **More Templates:** MySQL, MongoDB, MinIO, Plausible, PocketBase.
- [ ] **Service Networking:** Private internal networking between projects and services.

### 3. Team & Security
- [ ] **Team Management:** Invite users, Role-based Access Control (RBAC).
- [ ] **Audit Logs:** Track who did what and when (13.4).
- [ ] **2FA:** Two-factor authentication for local accounts.

### 4. Developer Experience
- [ ] **CLI Tool:** `goforge` CLI for deploying from terminal.
- [ ] **API Tokens:** Personal Access Tokens for external integrations.
- [ ] **Build Caching:** S3/Local cache for faster Docker builds.

## 🔮 V2.0: Multi-Server (Future)

- [ ] **Remote Agents:** Deploy to multiple servers via SSH/Agent.
- [ ] **Cluster Management:** Simple orchestration across nodes.
- [ ] **Load Balancing:** Distributed traffic handling.