# Monitoring

GoForge provides real-time monitoring for all deployed containers through the dashboard.

## Dashboard Overview

The main dashboard displays:

- **Total Projects** -- Number of projects you manage
- **Running Containers** -- Currently active containers
- **Active Services** -- One-click services that are running
- **Recent Deployments** -- Latest deployment activity

## Container Metrics

For each running container, GoForge monitors:

| Metric | Description | Source |
|--------|-------------|--------|
| CPU Usage | Percentage of CPU time used | Docker Stats API |
| Memory Usage | Current memory consumption | Docker Stats API |
| Memory Limit | Container memory limit | Docker Stats API |
| Network RX | Bytes received | Docker Stats API |
| Network TX | Bytes transmitted | Docker Stats API |

### Real-Time Stats

Container metrics are streamed to the browser in real-time via Server-Sent Events (SSE):

1. Navigate to a running container
2. Stats update automatically every 2 seconds
3. No page refresh needed -- data streams continuously

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

### Docker System Status

The monitoring system tracks overall Docker host health:

- Container count (running, stopped)
- Image count and storage usage
- Volume count and storage usage

### System Prune

To reclaim disk space from unused Docker resources:

1. Navigate to **Settings** > **System**
2. Click **System Prune**

This removes:

- Stopped containers
- Dangling images
- Unused build cache

!!! warning
    System prune removes all dangling Docker resources on the host, not just those managed by GoForge. Use with caution on shared Docker hosts.

## Metrics Storage

Container metrics are stored in the `container_metrics` PostgreSQL table with configurable retention:

```bash
# Default: 7 days
METRICS_RETENTION=168h
```

!!! note "Cleanup"
    A cleanup function exists in the database but requires manual scheduling via pg_cron or an external cron job. Automatic cleanup is planned for a future release.
