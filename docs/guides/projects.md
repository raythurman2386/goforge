# Creating Projects

Projects are the core unit in GoForge. Each project represents an application that you want to build and deploy from a Git repository.

## Creating a Project

1. Navigate to **Projects** in the sidebar
2. Click **New Project**
3. Fill in the project details:
   - **Name** -- A descriptive name for your project
   - **Repository URL** -- The Git repository URL
   - **Git Source** -- Optional linked Git provider
   - **Default Branch** -- Branch to deploy from (defaults to `main`)
   - **Dockerfile Path** -- Path to Dockerfile (defaults to `Dockerfile`)
   - **Build Context** -- Docker build context directory (defaults to `.`)

## Connecting a Git Source

After creating a project, connect it to a Git repository:

1. Go to the project's **Settings** > **Git Source**
2. Choose your provider (GitHub, GitLab, or Gitea)
3. Authenticate with the provider
4. Select the repository and branch

### Supported Providers

| Provider | Authentication | Features |
|----------|---------------|----------|
| GitHub | OAuth token | Repos, branches, tags, file browser, webhooks |
| GitLab | Personal access token | Repos, branches, tags, webhooks |
| Gitea | Personal access token | Repos, branches, tags, webhooks |

## Environments

Each project starts with a default environment. Environments allow you to manage different configurations (e.g., staging vs. production).

### Managing Environments

1. Go to the project page
2. Click the **Environments** tab
3. Create or select an environment
4. Configure environment variables specific to that environment

### Environment Variables

Set configuration values for your application:

1. Navigate to the environment's **Variables** tab
2. Add key-value pairs
3. Toggle **Secret** for sensitive values (passwords, API keys)
4. Click **Save**

Secret variables are encrypted at rest using AES-GCM.

!!! tip "Bulk import"
    You can import environment variables from a `.env` file format:
    ```
    DATABASE_URL=postgres://...
    API_KEY=secret123
    DEBUG=false
    ```

## Project Settings

| Setting | Description |
|---------|-------------|
| Name | Display name for the project |
| Description | Optional project description |
| Repository URL | Git repository URL |
| Default Branch | Git branch to deploy from (default: `main`) |
| Dockerfile Path | Path to Dockerfile (default: `Dockerfile`) |
| Build Context | Docker build context directory (default: `.`) |
| Auto Deploy | Automatically deploy on git push |
| Custom Domain | Custom domain for the project |

!!! note "Port and Domain"
    **Port** and **Domain** are configured at the **Environment** level, not the project level. Each environment can have its own domain and port settings, allowing different configurations for staging vs. production.

## Deleting a Project

**Delete Project** on the Settings tab opens a dialog that asks you to type the project name. The server checks it too: a delete without the exact name answers `400` and removes nothing.

Deleting a project will:

1. Stop and remove all containers labeled with the project, including rollback-restored copies and stopped containers
2. Delete all deployments and logs
3. Remove all environments and variables
4. Delete the git source configuration
5. Remove the project record

At startup and at 15-minute intervals (each pass has a two-minute timeout) GoForge also reconciles `goforge.managed=true` app containers against the project and environment labels. If either parent no longer exists it removes the container and writes `container.orphan.remove` to Activity with source **System** when the parent is absent before reconcile. If its owner is recoverable the row is attributed to that user; otherwise an admin can find it with **All users**. One-click service containers do not have project/environment labels and are left alone. Deleting an environment through the service similarly removes all containers labeled with that environment first; if Docker cleanup or the associated DB-row removal fails, the parent record and a durable cleanup intent are kept so the deletion can be retried even when Docker no longer lists the host object.

Project/environment deletion and deploy/rollback creation share a per-project lifecycle coordinator. A create racing the delete either completes its DB recording before the delete sweep, or sees the delete and creates nothing; an unrecorded container is removed, with failed cleanup retained for the reconciler.

There is no standalone HTTP environment-delete route at present; environment deletion is a service-level operation.


!!! warning
    Project deletion is permanent and cannot be undone. All deployment history and container data will be lost.
