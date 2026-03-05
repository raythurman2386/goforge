# Data Flow

This page traces the flow of data through GoForge for key operations.

## Deployment Flow

When a user triggers a deployment, data flows through these components:

```mermaid
sequenceDiagram
    participant Browser
    participant Handler
    participant DeployService
    participant Worker
    participant Pipeline
    participant Git
    participant Docker
    participant DB
    participant SSE

    Browser->>Handler: POST /projects/{id}/deploy
    Handler->>DeployService: CreateDeployment(deployment *models.Deployment)
    DeployService->>DB: Insert deployment (status: pending)
    DeployService->>Worker: Enqueue(deploymentID)
    DeployService-->>Handler: deployment ID
    Handler-->>Browser: Redirect to deployment page
    
    Browser->>SSE: Subscribe to deployment:{id}
    
    Worker->>Pipeline: RunPipeline(deployment)
    Pipeline->>DB: Transition to "cloning"
    Pipeline->>SSE: Log "Cloning repository..."
    Pipeline->>Git: Clone(repoURL, branch)
    Git-->>Pipeline: Clone complete
    
    Pipeline->>DB: Transition to "building"
    Pipeline->>SSE: Log "Building Docker image..."
    Pipeline->>Docker: BuildImage(context, dockerfile)
    Docker-->>Pipeline: Image built
    
    Pipeline->>DB: Transition to "deploying"
    Pipeline->>SSE: Log "Starting container..."
    Pipeline->>Docker: CreateContainer(image, options)
    Docker-->>Pipeline: Container started
    
    Pipeline->>DB: Transition to "running"
    Pipeline->>SSE: Log "Deployment successful"
    
    SSE-->>Browser: Real-time status updates
```

## Authentication Flow

### Local Login

```mermaid
sequenceDiagram
    participant Browser
    participant Handler
    participant Auth
    participant SessionMgr
    participant DB

    Browser->>Handler: POST /auth/login (email, password)
    Handler->>Auth: Authenticate(email, password)
    Auth->>DB: GetByEmail(email)
    DB-->>Auth: User record
    Auth->>Auth: ComparePassword(hash, password)
    Auth-->>Handler: User (or error)
    
    Handler->>SessionMgr: CreateSession(userID)
    SessionMgr->>SessionMgr: Generate random token
    SessionMgr->>SessionMgr: SHA-256 hash token
    SessionMgr->>DB: Store hashed token + metadata
    SessionMgr-->>Handler: Plain token
    
    Handler->>Handler: SetSessionCookie(token)
    Handler-->>Browser: Redirect to dashboard + Set-Cookie
```

### GitHub OAuth

```mermaid
sequenceDiagram
    participant Browser
    participant Handler
    participant GitHub
    participant SessionMgr
    participant DB

    Browser->>Handler: GET /auth/github
    Handler->>Handler: Generate OAuth state
    Handler-->>Browser: Redirect to GitHub authorize URL
    
    Browser->>GitHub: User authorizes
    GitHub-->>Browser: Redirect to callback with code
    
    Browser->>Handler: GET /auth/github/callback?code=xxx
    Handler->>GitHub: Exchange code for token
    GitHub-->>Handler: Access token
    Handler->>GitHub: Get user info
    GitHub-->>Handler: User profile + email
    
    Handler->>DB: FindOrCreate user by GitHub ID
    Handler->>DB: Encrypt and store access token
    Handler->>SessionMgr: CreateSession(userID)
    Handler-->>Browser: Redirect to dashboard + Set-Cookie
```

## Request Middleware Chain

Every HTTP request passes through middleware in this order:

```mermaid
graph LR
    A[Request] --> B[Recovery]
    B --> C[Rate Limiter]
    C --> D[Session Loader]
    D --> E[CSRF Check]
    E --> F[Handler]
    F --> G[Template Render]
    G --> H[Response]
```

## Real-Time Data (SSE)

Container stats and deployment logs stream to the browser via SSE:

```mermaid
sequenceDiagram
    participant Browser
    participant SSEHandler
    participant Docker

    Browser->>SSEHandler: GET /containers/{id}/stats
    
    loop Every 2 seconds
        SSEHandler->>Docker: ContainerStats(id)
        Docker-->>SSEHandler: CPU, Memory, Network
        SSEHandler-->>Browser: SSE event with rendered stats HTML
    end
    
    Browser->>SSEHandler: Connection closed
```

!!! note
    Container stats are polled directly by the SSE handler every 2 seconds and do not flow through the SSE Hub. The handler renders stats as HTML (using templ components) and streams them directly to the client.

## Service Deployment Flow

One-click services follow a template-based deployment:

```mermaid
sequenceDiagram
    participant Browser
    participant Handler
    participant InstanceSvc
    participant Catalog
    participant Docker
    participant DB

    Browser->>Handler: POST /services/deploy
    Handler->>Catalog: GetTemplate(serviceType)
    Catalog-->>Handler: Template (ports, env, volumes)
    
    Handler->>InstanceSvc: DeployInstance(template, config)
    InstanceSvc->>InstanceSvc: Interpolate template variables
    InstanceSvc->>Docker: PullImage(template.image)
    InstanceSvc->>Docker: CreateContainer(options)
    Docker-->>InstanceSvc: Container ID
    
    InstanceSvc->>DB: Save ServiceInstance record
    InstanceSvc-->>Handler: Instance details
    Handler-->>Browser: Render service page
```

## Database Entity Relationships

```mermaid
erDiagram
    User ||--o{ Project : owns
    User ||--o{ Session : has
    User ||--o{ ServiceInstance : owns
    
    Project ||--o{ Environment : has
    Project ||--o{ Deployment : has
    Project ||--o| GitSource : has
    
    Environment ||--o{ EnvVariable : contains
    Environment ||--o{ Deployment : targets
    
    Deployment ||--o| Container : creates
    Deployment ||--o{ DeploymentLog : generates
```
