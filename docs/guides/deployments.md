# Deployments

Deployments are the process of building and running your application from source code.

## Triggering a Deployment

### Manual Deployment

1. Navigate to your project
2. Click **Deploy** or **New Deployment**
3. Select the environment and branch
4. GoForge will clone, build, and deploy your application

### Webhook-Triggered Deployment

GoForge supports automatic deployments via Git webhooks:

1. Go to project **Settings** > **Webhooks**
2. Copy the webhook URL
3. Add it to your Git provider:
   - **GitHub:** Repository Settings > Webhooks > Add webhook
   - **GitLab:** Repository Settings > Webhooks > Add webhook
   - **Gitea:** Repository Settings > Webhooks > Add Gitea webhook
4. Set the content type to `application/json`
5. Select **Push events**

When you push to the configured branch, GoForge automatically triggers a deployment.

### Webhook Security

Webhooks are verified using HMAC-SHA256 signatures:

- GitHub: `X-Hub-Signature-256` header
- GitLab: `X-Gitlab-Token` header
- Gitea: `X-Gitea-Signature` header

## Deployment Pipeline

Each deployment goes through these stages:

### 1. Pending

The deployment is queued and waiting for a worker slot. Only `MAX_CONCURRENT_BUILDS` deployments can run simultaneously (default: 2).

### 2. Cloning

GoForge clones your Git repository to a temporary build directory.

### 3. Building

Docker builds an image from your Dockerfile:

```dockerfile
# Example Dockerfile for a Go application
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o main .

FROM alpine:latest
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

The container is running and serving traffic. Previous deployment containers are cleaned up.

## Monitoring Deployments

### Real-Time Logs

Deployment logs stream in real-time via Server-Sent Events:

- Build output from Docker
- Container startup logs
- Status transition events

Navigate to the deployment detail page to see live logs.

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

Deployments are processed through a worker queue:

- Queue size is configurable
- Only one deployment per project runs at a time
- New deployments for the same project queue behind the current one
