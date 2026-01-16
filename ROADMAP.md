# Roadmap

This roadmap tracks the development of GoForge towards its MVP release. It is derived from the Technical Design Specification.

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
- [x] **Pagination Component** (4.7) - Implemented with full navigation, page ranges, per-page selector

### Phase 5: Project Management
- [x] Basic CRUD, environments, deletion.
- [x] **Environment Switcher** (5.7) - Implemented via query params and dropdown
- [x] **Secret Masking** (5.10) - AES-GCM encryption, auto-detection, log sanitization
- [x] **Project Settings Page** (5.11) - Implemented with delete and variable management
- [ ] **Search/Filter** (5.13) - *Low Priority*

### Phase 6: Git Integration
- [x] GitHub client, git operations, source config.
- [ ] **GitLab & Gitea Clients** (6.3, 6.4) - *Medium Priority*
- [x] **SSH Key Management** (6.5) - Implemented with Ed25519 key generation and go-git auth
- [ ] **Repo Browser & Branch Selector** (6.7, 6.8) - *Medium Priority*
- [x] **Webhook Receiver & Verification** (6.9, 6.10) - Implemented with signature verification
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
- [x] **Blue-Green Deployment** (8.6) - Implemented with health check verification and auto-rollback
- [x] **Health Check Verification** (8.7) - HTTP/TCP health checks with retry logic
- [x] **Rollback Mechanism** (8.8) - RollbackManager with validation and container restoration
- [x] **Deployment History & Detail UI** (8.10, 8.11) - List and detail pages with status badges
- [x] **Manual Trigger** (8.12) - Implemented via UI button and background worker

### Phase 9: Traefik Integration
- [x] Basic Traefik setup.
- [x] **Docker Labels Generation** (9.2) - Full label generation with middlewares
- [x] **Let's Encrypt & Domain Validation** (9.3, 9.4) - ACME config, DNS validation
- [x] **Custom Domain UI** (9.5) - Domain management service with verification
- [ ] **SSL Status & Redirects** (9.6, 9.8) - *Medium Priority*

### Phase 10: Real-Time Features (SSE)
- [x] Hub & HTMX integration.
- [x] **SSE Client Handler** (10.2) - Implemented topic-based registration in SSE Hub
- [x] **Log Streaming Endpoints** (10.4, 10.5, 10.6) - Implemented for deployments, containers, and builds
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
- [x] **Template Schema** (12.2) - Implemented via struct tags and validation
- [ ] **Directory Watching** (12.3) - *Medium Priority*
- [ ] **Additional Templates** (MySQL, Mongo, etc.) (12.6 - 12.9) - *Low Priority*
- [ ] **Connection String Exposure** (12.13) - *High Priority*
- [ ] **Backup/Restore** (12.14) - *Medium Priority*

## Next Steps (Prioritized)

1.  **SSE Log Streaming:** Implement log streaming endpoints (10.4, 10.5, 10.6) for real-time deployment logs.
2.  **Blue-Green Deployment:** Implement zero-downtime deployments (8.6).
3.  **SSH Key Management:** Enable private repo cloning (6.5).
4.  **Docker Compose Support:** Parse and deploy multi-container apps (7.4).
5.  **Connection String Exposure:** Expose service connection details to apps (12.13).

## Phase 13: Polish & Production Readiness (Upcoming)
- Rate limiting, audit logging, systemd files, backups, E2E tests.
