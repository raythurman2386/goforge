# GoForge v1 Completion PRD

## HR Eng

| GoForge v1 Completion PRD |  | Finish the remaining features of GoForge to reach v1 release status, focusing on Traefik integration, Monitoring, and advanced Git workflows. |
| :---- | :---- | :---- |
| **Author**: Pickle Rick **Contributors**: Ray Thurman **Intended audience**: Engineering | **Status**: Draft **Created**: 2026-01-16 | **Self Link**: [Local] **Context**: SPECIFICATION.md |

## Introduction

GoForge is currently a "work in progress" with the core foundation laid but critical features missing for a production-ready PaaS. This PRD defines the scope to complete the remaining phases (6-13) of the specification, ensuring a fully functional, self-hosted deployment platform.

## Problem Statement

**Current Process:** Users can do basic deployments, but lack SSL (Traefik integration), resource monitoring, advanced git controls (branch selection), and reliable production features (backups, cleanup).
**Primary Users:** Developers self-hosting their infrastructure.
**Pain Points:**
- No SSL/HTTPS (Security risk).
- No visibility into container resources (Blind flying).
- No auto-deploy from webhooks (Manual toil).
- Incomplete service catalog.
**Importance:** Without these features, GoForge is a toy, not a tool. v1 release requires feature parity with the core promise of "Coolify alternative".

## Objective & Scope

**Objective:** Complete all unchecked items in `SPECIFICATION.md` to reach a stable v1.0 release.
**Ideal Outcome:** A user can install GoForge, connect a repo, deploy with SSL, monitor stats, and sleep soundly.

### In-scope or Goals
- **Phase 6 (Git):** Webhooks, Branch Selection, Git/Gitea support.
- **Phase 7 (Docker):** Compose parser, pruning, event listening.
- **Phase 9 (Traefik):** Full integration, SSL generation, Custom Domains.
- **Phase 10 (Real-time):** Status updates, stability.
- **Phase 11 (Monitoring):** Stats collection and visualization.
- **Phase 13 (Polish):** Rate limiting, Systemd, Documentation.

### Not-in-scope or Non-Goals
- Multi-server architecture (Cluster).
- Kubernetes support.
- SaaS billing integration.

## Product Requirements

### Critical User Journeys (CUJs)
1.  **Secure Deployment**: User deploys a project -> GoForge configures Traefik -> Project is accessible via HTTPS.
2.  **Continuous Deployment**: User pushes to GitHub -> Webhook triggers GoForge -> GoForge redeploys the specific service.
3.  **Observability**: User views Project Dashboard -> Sees real-time CPU/Memory graphs and live logs.

### Functional Requirements

| Priority | Requirement | User Story |
| :---- | :---- | :---- |
| P0 | Traefik & SSL Integration | As a user, I want my apps to have automatic HTTPS so they are secure. |
| P0 | Webhook Auto-Deploy | As a user, I want git pushes to trigger deploys automatically. |
| P1 | Resource Monitoring | As a user, I want to see how much RAM my app uses. |
| P1 | Branch/Tag Selection | As a user, I want to choose which branch to deploy. |
| P2 | Docker Compose Support | As a user, I want to deploy complex multi-container apps. |
| P2 | System Cleanup | As a user, I want old images pruned to save disk space. |

## Assumptions
- Traefik is running in a container on the same network.
- The host has a public IP for Let's Encrypt validation.

## Risks & Mitigations
- **Risk**: Traefik configuration complexity. **Mitigation**: Start with simple label generation, add middleware later.
- **Risk**: Resource monitoring overhead. **Mitigation**: Poll efficiently (e.g., every 5s) and aggregate.

## Business Benefits/Impact/Metrics
- **Feature Completeness**: 100% of Spec.
- **Stability**: Zero manual interventions for standard deploys.

## Stakeholders / Owners
- **Engineering**: Pickle Rick
