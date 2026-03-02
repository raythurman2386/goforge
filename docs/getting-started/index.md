# Getting Started

Welcome to GoForge. This section will help you get GoForge up and running, whether you are setting up a local development environment or preparing for production deployment.

## What is GoForge?

GoForge is a self-hosted Platform as a Service (PaaS) that turns your server into a deployment platform similar to Heroku or Railway. It connects to your Git repositories, builds Docker images from your code, and deploys them with automatic SSL certificates and reverse proxy routing via Traefik.

## How It Works

1. **Connect** -- Link your GitHub, GitLab, or Gitea account
2. **Create** -- Set up a project pointing to your repository
3. **Deploy** -- GoForge clones your repo, builds a Docker image, and runs the container
4. **Route** -- Traefik automatically provisions SSL certificates and routes traffic to your container
5. **Monitor** -- Watch real-time logs and resource usage from the dashboard

## Sections

- [Prerequisites](prerequisites.md) -- What you need before installing GoForge
- [Quick Start](quickstart.md) -- Get a development instance running in minutes
- [Docker Compose Setup](docker-compose.md) -- Full stack deployment with Docker Compose
