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

Deleting a project will:

1. Stop and remove all running containers
2. Delete all deployments and logs
3. Remove all environments and variables
4. Delete the git source configuration
5. Remove the project record

!!! warning
    Project deletion is permanent and cannot be undone. All deployment history and container data will be lost.
