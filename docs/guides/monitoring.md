# Monitoring

GoForge provides real-time monitoring for all deployed containers through the dashboard.

## Dashboard Overview

The main dashboard displays:

- **Total Projects** -- Number of projects you manage
- **Active Services** -- One-click services that are running
- **Running Containers** -- Running and total containers
- **Resource Usage** -- CPU and memory usage
- **Docker Disk Usage** -- Images, containers, volumes, and build cache

## Container Metrics

For each running container, GoForge monitors:

| Metric | Description | Source |
|--------|-------------|--------|
| CPU Usage | Percentage of CPU time used | Docker Stats API |
| Memory Usage | Current memory consumption | Docker Stats API |
| Memory Limit | Container memory limit | Docker Stats API |
| Network RX | Bytes received | Docker Stats API |
| Network TX | Bytes transmitted | Docker Stats API |
| Block read | Bytes read from block devices | Docker Stats API |
| Block write | Bytes written to block devices | Docker Stats API |

### Real-Time Stats

Container metrics are streamed to the browser in real-time via Server-Sent Events (SSE):

1. Open a running container, a service's detail page, or a project's Overview
2. The first sample arrives about half a second after connect, then stats update every 2 seconds
3. No page refresh needed -- data streams continuously. A stopped or missing container shows "unavailable"

Service detail and project Overview also show a **Container** card: image, state, health, uptime, restarts, limits, ports, the Traefik host rule, and a **View logs** link.

## Container Logs

View container stdout/stderr output in real-time:

1. Go to the container or deployment detail page
2. Logs stream automatically via SSE
3. Build output, application logs, and errors are all visible

### Log Options

| Option | Description |
|--------|-------------|
| Follow | Stream new log lines as they appear |
| Timestamps | Show or hide log timestamps |

## Deployment Logs

Each deployment stage generates logs:

- **Clone logs** -- Git clone progress
- **Build logs** -- Docker build output (layer-by-layer)
- **Deploy logs** -- Container creation and startup

These are accessible from the deployment detail page.

## System Health

### Health Check Endpoint

GoForge exposes a health check endpoint:

```
GET /health
```

Returns `200 OK` when the server is running.

```
GET /health/ready
```

Checks the database and Docker. Returns `200` when both are reachable and `503` otherwise, with a JSON body of per-check results. Both endpoints are public, and the `/health/ready` body includes the error text (see `SECURITY.md`).

### Docker System Status

The monitoring system tracks overall Docker host health:

- Container count (running, stopped)
- Image count and storage usage
- Volume count and storage usage

### System Prune

To reclaim disk space from unused Docker resources, an admin clicks **Prune System** on the Dashboard (`POST /dashboard/prune`; `POST /settings/prune` does the same).

This removes, for containers and images that carry the GoForge managed label only:

- Stopped containers
- Unused images (tagged or dangling)

Volumes, networks, build cache, and anything GoForge did not create are left alone.

## Metrics Storage

Container metrics are not stored. They are read from Docker and streamed live. The `container_metrics` table exists but nothing writes to it, and `ENABLE_METRICS` and `METRICS_RETENTION` are accepted but unused. There is no `/metrics` endpoint.

!!! note "Cleanup"
    Migration 010 adds a `cleanup_old_data()` database function (container metrics older than 7 days, deployment logs older than 30 days). Nothing schedules it; run it from pg_cron or an external cron job if you want deployment logs trimmed.
