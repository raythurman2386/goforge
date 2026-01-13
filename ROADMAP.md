# Roadmap

This roadmap tracks the development of GoCool towards its MVP release. It is derived from the Technical Design Specification.

## Completed Phases

- [x] **Phase 1: Project Foundation**
  - Fully initialized with config, logger, router, and basic structure.
- [x] **Phase 2: Database & Models**
  - PostgreSQL connection, migrations, and repositories implemented.
- [x] **Phase 3: Authentication**
  - GitHub OAuth, local auth, sessions, and middleware complete.

## In Progress / Partial Completion

### Phase 4: Core UI Components
- [x] Basic components (nav, sidebar, toast, modal, form, table, loading, empty, errors)
- [ ] **Pagination Component** (4.7) - *High Priority*

### Phase 5: Project Management
- [x] Basic CRUD, environments, deletion.
- [ ] **Environment Switcher** (5.7) - *Medium Priority*
- [ ] **Secret Masking** (5.10) - *High Priority (Security)*
- [ ] **Project Settings Page** (5.11) - *Medium Priority*
- [ ] **Search/Filter** (5.13) - *Low Priority*

### Phase 6: Git Integration
- [x] GitHub client, git operations, source config.
- [ ] **GitLab & Gitea Clients** (6.3, 6.4) - *Medium Priority*
- [ ] **SSH Key Management** (6.5) - *High Priority*
- [ ] **Repo Browser & Branch Selector** (6.7, 6.8) - *Medium Priority*
- [ ] **Webhook Receiver & Verification** (6.9, 6.10) - *High Priority*
- [ ] **Shallow Clone** (6.12) - *Low Priority (Optimization)*

### Phase 7: Docker Integration
- [x] Client, build, container, logs.
- [ ] **Connection Health Check** (7.2) - *Medium Priority*
- [ ] **Docker Compose Support** (7.4) - *High Priority*
- [ ] **Network & Volume Management** (7.7, 7.8) - *Medium Priority*
- [ ] **Image Cleanup** (7.9) - *Low Priority (Maintenance)*
- [ ] **Docker Event Listener** (7.10) - *Medium Priority*

### Phase 8: Deployment Pipeline
- [x] Service, state, queue, concurrent limits, basic pipeline.
- [ ] **Blue-Green Deployment** (8.6) - *High Priority (Reliability)*
- [ ] **Health Check Verification** (8.7) - *High Priority*
- [ ] **Rollback Mechanism** (8.8) - *High Priority*
- [ ] **Deployment History & Detail UI** (8.10, 8.11) - *High Priority*
- [ ] **Manual Trigger** (8.12) - *Medium Priority*

### Phase 9: Traefik Integration
- [x] Basic Traefik setup.
- [ ] **Docker Labels Generation** (9.2) - *Critical*
- [ ] **Let's Encrypt & Domain Validation** (9.3, 9.4) - *Critical*
- [ ] **Custom Domain UI** (9.5) - *High Priority*
- [ ] **SSL Status & Redirects** (9.6, 9.8) - *Medium Priority*

### Phase 10: Real-Time Features (SSE)
- [x] Hub & HTMX integration.
- [ ] **SSE Client Handler** (10.2) - *High Priority*
- [ ] **Log Streaming Endpoints** (10.4, 10.5, 10.6) - *High Priority*
- [ ] **Status Updates** (10.7) - *Medium Priority*
- [ ] **Reconnection Handling** (10.9) - *Medium Priority*
- [ ] **Connection Status Indicator** (10.10) - *Low Priority*

### Phase 11: Resource Monitoring
- [x] Basic stats structure.
- [ ] **Periodic Polling & Storage** (11.2, 11.3) - *Medium Priority*
- [ ] **Retention Policy** (11.4) - *Low Priority*
- [ ] **UI Charts (CPU, Memory, Disk)** (11.5 - 11.7) - *Low Priority (Nice to have)*
- [ ] **Container Health Indicators** (11.8) - *Medium Priority*
- [ ] **Alerts** (11.9) - *Low Priority*

### Phase 12: One-Click Services
- [x] Catalog, instance management, PostgreSQL/Redis templates.
- [ ] **Template Schema & Directory Watching** (12.2, 12.3) - *Medium Priority*
- [ ] **Additional Templates** (MySQL, Mongo, etc.) (12.6 - 12.9) - *Low Priority*
- [ ] **Connection String Exposure** (12.13) - *High Priority*
- [ ] **Backup/Restore** (12.14) - *Medium Priority*

## Next Steps (Prioritized)

1.  **Deployment Reliability:** Implement Rollback (8.8) and Health Check Verification (8.7).
2.  **Traefik & SSL:** Complete label generation (9.2) and Let's Encrypt config (9.3).
3.  **Git Webhooks:** Implement receiver (6.9) to enable push-to-deploy.
4.  **UI Gaps:** Finish Deployment History (8.10) and Pagination (4.7).
5.  **Secrets:** Implement encryption for secrets (5.10).

## Phase 13: Polish & Production Readiness (Upcoming)
- Rate limiting, audit logging, systemd files, backups, E2E tests.
