# GoForge

> **⚠️ Security Warning: GoForge is in active development.** 
> This is a self-hosted PaaS that runs containerized workloads. Review the [Security Guide](security.md) before production use.

---

**A lightweight, self-hosted Platform as a Service (PaaS) built in Go.**

GoForge provides git-to-deploy workflows, one-click services, automatic SSL via Cloudflare/Traefik, and real-time monitoring. Think of it as a self-hosted alternative to Heroku or Railway that you run on your own infrastructure.

---

## Key Features

- **Git-to-Deploy** -- Push code to your repository and GoForge builds and deploys Docker containers automatically via webhooks.
- **Multi-Provider Git Integration** -- Supports GitHub, GitLab, and Gitea for repository management, branch selection, and webhook-triggered deployments.
- **One-Click Services** -- Deploy PostgreSQL, Redis, and other services from a template catalog with a single click.
- **Automatic SSL** -- Cloudflare Tunnel + Traefik handles certificates and routing automatically.
- **Real-Time Monitoring** -- Live CPU, memory, and network stats for all containers via Server-Sent Events.
- **Blue-Green Deployments** -- Zero-downtime deployments with health check verification and automatic rollback.
- **Authentication** -- Local email/password authentication (Argon2id) and GitHub OAuth.
- **Secrets Management** -- AES-256-GCM encryption for access tokens, SSH keys, and environment variables.
- **Audit Logging** -- Track all administrative actions across your platform.
- **HTMX Frontend** -- Fast, server-rendered UI with real-time updates using HTMX 2 and templ templates.

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.26+ |
| Database | PostgreSQL |
| Containerization | Docker |
| Reverse Proxy | Traefik |
| Frontend | HTMX 2, templ, TailwindCSS |
| Authentication | Argon2id, GitHub OAuth |
| Real-Time | Server-Sent Events |

## Quick Links

- [Quick Start](getting-started/quickstart.md) -- Get GoForge running in minutes
- [Architecture Overview](architecture/index.md) -- Understand how GoForge is built
- [Configuration Reference](configuration/index.md) -- All environment variables explained
- [Production Deployment](deployment/index.md) -- Deploy GoForge to your server
- [Development Guide](development/index.md) -- Contribute to GoForge

## Project Status

GoForge is currently at **v0.1.0** and is under active development. See the [Roadmap](https://github.com/raythurman2386/goforge/blob/main/ROADMAP.md) for planned features.

## License

GoForge is open source software licensed under the MIT License.
