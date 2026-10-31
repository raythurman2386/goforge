# Deployments

Deployments are the process of building and running your application from source code.

## Triggering a Deployment

### Manual Deployment

1. Navigate to your project
2. Click **Deploy** or **New Deployment**
3. Select the environment and branch
4. GoForge will clone, build, and deploy your application

### Webhook-Triggered Deployment

GoForge accepts push webhooks at one shared secret (`WEBHOOK_SECRET`):

- `POST /webhooks/github`
- `POST /webhooks/gitlab`
- `POST /webhooks/gitea`

Add that URL on the Git provider and select push events. There is no per-project webhook settings page. GitHub and Gitea payloads are checked with HMAC-SHA256. GitLab sends the secret in `X-Gitlab-Token`.

### Webhook Security

Webhooks are verified using HMAC-SHA256 signatures:

- GitHub: `X-Hub-Signature-256` header
- GitLab: `X-Gitlab-Token` header
- Gitea: `X-Gitea-Signature` header

## Deployment Pipeline

Each deployment goes through these stages:

### 1. Pending

The deployment is queued. The worker runs one deployment at a time. `MAX_CONCURRENT_BUILDS` is loaded and currently unused.

### 2. Cloning

GoForge clones your Git repository to a temporary build directory.

### 3. Building

Docker builds an image from the project's Dockerfile. `build_context` is the directory sent to Docker, relative to the repository root. `dockerfile_path` is relative to that context. A path that leaves the repository is rejected, and a missing Dockerfile fails the deployment with `dockerfile not found`.

```dockerfile
# Example Dockerfile for a Go application
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o main .

FROM alpine:3.24
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/main /main
EXPOSE 8080
CMD ["/main"]
```

### 4. Deploying

GoForge creates and starts a Docker container from the built image with:

- Traefik labels for routing and SSL
- Environment variables from the project's environment
- Network attachment to the Traefik network

### 5. Running

The container is running and serving traffic. The previous deployment's container keeps running for a 5-minute grace period, so during that window `docker ps` (and the dashboard's **Running Containers**) shows both. After the grace period GoForge stops and removes the previous container; its record is kept as `exited` so a rollback can rebuild it from the image. The GoForge log shows `retired previous deployment container` for each one. The grace timer lives in the GoForge process: if GoForge restarts within those 5 minutes, the previous container is not retired and must be removed by hand.

## Monitoring Deployments

### Real-Time Logs

Clone and build output is stored in `deployment_logs`. The deployment page renders those lines, and the log stream replaces that block when it connects so a reconnect does not duplicate them. New lines append after that. The status badges on the same page update from the stream. Container stdout is a separate stream on the container page.

### Deployment Status

| Status | Description |
|--------|-------------|
| `pending` | Queued, waiting for worker |
| `cloning` | Cloning Git repository |
| `building` | Building Docker image |
| `deploying` | Creating and starting container |
| `running` | Successfully deployed and serving |
| `failed` | An error occurred during any stage |
| `canceled` | Manually canceled by user |
| `rolled_back` | Replaced by a rollback |

## Rollbacks

If a deployment causes issues, you can roll back to a previous deployment:

1. Go to the project's **Deployments** tab
2. Find a previous successful deployment
3. Click **Rollback to this deployment**

The rollback process:

1. Stops the current container
2. Restores the previous deployment's container configuration
3. Starts a new container with the previous image and settings

## Deployment Timeouts

Deployments have a configurable timeout (`BUILD_TIMEOUT`, default: 15 minutes). If the pipeline does not complete within this time, the deployment is marked as failed.

## Queue Behavior

Deployments are processed through a PostgreSQL-backed persistent queue:

- The queue is persistent — deployments survive server restarts
- On startup, any orphaned jobs (from a previous crash) are automatically requeued
- Deployments are processed one at a time in order
- Canceling a queued deployment removes it. Canceling a running build stops that build and leaves the status `canceled`
- Queue state is visible in the database for monitoring
