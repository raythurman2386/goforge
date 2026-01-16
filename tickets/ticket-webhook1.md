# Feature: Git Webhooks

## Status
- [ ] Created

## Description
Implement webhook receivers for GitHub, GitLab, and Gitea to automatically trigger deployments on push events.

## Requirements
1.  **Endpoints**:
    -   `POST /webhooks/github`
    -   `POST /webhooks/gitlab`
    -   `POST /webhooks/gitea`
2.  **Security**:
    -   Verify signatures for GitHub (`X-Hub-Signature-256`) and Gitea (`X-Gitea-Signature`).
    -   Verify secret tokens for GitLab (`X-Gitlab-Token`).
3.  **Logic**:
    -   Parse push events.
    -   Ignore tags (unless configured otherwise).
    -   Match repository URL to existing Projects/Services.
    -   Check if the pushed branch matches the configured deployment branch.
    -   Trigger a deployment if conditions are met.
4.  **Database**:
    -   Need a method to find projects by repository URL.

## Tasks
- [x] Add webhook routes to `internal/web/router.go` (public routes).
- [x] Implement `GetProjectByRepoURL` in `internal/database/repositories/project.go`.
- [x] Implement Gitea handler in `internal/webhook/webhook.go` (handling `X-Gitea-Signature`).
- [x] Implement `processPushEvent` to look up project and trigger deployment.
- [x] Add tests for Gitea handler and deployment triggering logic.
