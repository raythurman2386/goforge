# Git Integration

GoForge integrates with GitHub, GitLab, and Gitea for repository management, branch selection, and webhook-triggered deployments.

## Supported Providers

| Provider | Auth Method | Repo Browser | Webhooks | File Content |
|----------|------------|--------------|----------|-------------|
| GitHub | OAuth token | Yes | Yes | Yes |
| GitLab | Personal access token | Yes | Yes | Yes |
| Gitea | Personal access token | Yes | Yes | Yes |

## GitHub Integration

### Authentication

GitHub uses OAuth tokens obtained during the login flow. When you log in with GitHub, GoForge stores your access token (encrypted) and uses it for API calls.

### Repository Browser

The Git integration provides an in-app repository browser:

- List all accessible repositories
- Browse branches and tags
- View file contents and Dockerfile detection

### Webhooks

GoForge can register webhooks with GitHub to trigger automatic deployments on push:

1. GoForge creates a webhook on your repository
2. GitHub sends a POST request to GoForge on each push
3. GoForge verifies the HMAC-SHA256 signature
4. If the push matches the configured branch, a deployment is triggered

## GitLab Integration

### Setup

1. Generate a Personal Access Token at **GitLab** > **Settings** > **Access Tokens**
2. Required scopes: `api`, `read_repository`
3. Add the token in GoForge project settings

### Self-Hosted GitLab

GoForge supports self-hosted GitLab instances. When configuring the git source, provide the base URL of your GitLab instance (e.g., `https://gitlab.company.com`).

## Gitea Integration

### Setup

1. Generate an API token at **Gitea** > **Settings** > **Applications**
2. Add the token in GoForge project settings

### Self-Hosted Gitea

Like GitLab, Gitea can be self-hosted. Provide the base URL of your Gitea instance when configuring the git source.

## SSH Key Support

GoForge can generate Ed25519 SSH key pairs for accessing private repositories:

1. Go to project **Settings** > **Git Source**
2. Choose **SSH** authentication
3. GoForge generates an Ed25519 key pair
4. Add the public key to your Git provider as a deploy key
5. GoForge uses the private key (encrypted at rest) for clone operations

!!! note "Current limitation"
    SSH-based cloning is supported for key generation and storage, but the clone operation currently uses HTTPS with token authentication. Full SSH clone support is planned.

## Repository Cloning

When a deployment is triggered, GoForge clones the repository:

1. Creates a temporary directory for the build
2. Clones the repository at the specified branch/commit
3. Builds a Docker image from the Dockerfile
4. Cleans up the temporary directory after build

### Clone Authentication

Every provider uses the same code path (`internal/git/operations.go`): when the project's Git source has an access token, the clone sends it as the HTTP basic password with the username `x-access-token`. A public repository clones with no token.

| Provider | Method |
|----------|--------|
| GitHub | OAuth or access token, HTTP basic auth |
| GitLab | Personal access token, HTTP basic auth |
| Gitea | Personal access token, HTTP basic auth |

## Webhook Verification

GoForge verifies webhook signatures to prevent unauthorized deployment triggers:

| Provider | Header | Algorithm |
|----------|--------|-----------|
| GitHub | `X-Hub-Signature-256` | HMAC-SHA256 |
| GitLab | `X-Gitlab-Token` | Constant-time token comparison |
| Gitea | `X-Gitea-Signature` (or `X-Hub-Signature-256`) | HMAC-SHA256 |
