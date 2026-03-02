# Traefik & SSL Configuration

GoForge uses Traefik as its reverse proxy for automatic SSL certificate provisioning and traffic routing.

## How It Works

1. GoForge creates Docker containers with Traefik labels
2. Traefik discovers containers via the Docker provider
3. Traefik automatically generates Let's Encrypt SSL certificates
4. Traffic is routed from `https://app.example.com` to the correct container

## Docker Compose Configuration

The `docker-compose.yml` includes a pre-configured Traefik service:

```yaml
traefik:
  image: traefik:v3.6
  command:
    - "--providers.docker=true"
    - "--providers.docker.exposedbydefault=false"
    - "--entrypoints.web.address=:80"
    - "--entrypoints.websecure.address=:443"
    - "--certificatesresolvers.letsencrypt.acme.email=${ACME_EMAIL}"
    - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
    - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
  ports:
    - "80:80"
    - "443:443"
  volumes:
    - "/var/run/docker.sock:/var/run/docker.sock:ro"
    - "traefik_letsencrypt:/letsencrypt"
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TRAEFIK_NETWORK` | `traefik` | Docker network for service discovery |
| `ACME_EMAIL` | (empty in app code; `admin@example.com` in docker-compose.yml) | Email for Let's Encrypt registration |

!!! note "Compose-only variables"
    The following variables are used only in `docker-compose.yml` and are not part of GoForge's Go application configuration:
    `TRAEFIK_DASHBOARD_PORT`, `TRAEFIK_DASHBOARD_INSECURE`, `TRAEFIK_LOG_LEVEL`, `GOFORGE_DOMAIN`.

## Traefik Labels

GoForge generates Traefik labels for deployed containers. The label generation includes:

### Routing

- Host-based routing (`Host(\`app.example.com\`)`)
- HTTP-to-HTTPS redirect middleware
- Let's Encrypt certificate resolver

### Security Middleware

GoForge can generate labels for these Traefik middleware options:

| Middleware | Purpose |
|-----------|---------|
| Rate Limiting | Limit requests per period |
| Security Headers | HSTS, CSP, X-Frame-Options, etc. |
| Basic Auth | HTTP Basic Authentication |
| IP Whitelist | Restrict access by IP |
| Sticky Sessions | Session affinity for load balancing |

### Example Labels

For a deployed application at `myapp.example.com`:

```
traefik.enable=true
traefik.http.routers.myapp.rule=Host(`myapp.example.com`)
traefik.http.routers.myapp.entrypoints=websecure
traefik.http.routers.myapp.tls.certresolver=letsencrypt
traefik.http.services.myapp.loadbalancer.server.port=3000
traefik.http.routers.myapp-http.rule=Host(`myapp.example.com`)
traefik.http.routers.myapp-http.entrypoints=web
traefik.http.routers.myapp-http.middlewares=myapp-redirect
traefik.http.middlewares.myapp-redirect.redirectscheme.scheme=https
```

## SSL Certificates

### Let's Encrypt (Default)

By default, Traefik uses Let's Encrypt for automatic certificate provisioning:

- Certificates are obtained via HTTP-01 challenge
- Stored in the `traefik_letsencrypt` Docker volume
- Automatically renewed before expiration

### Requirements for SSL

1. Your domain must have DNS pointing to your server's public IP
2. Ports 80 and 443 must be accessible from the internet
3. A valid email must be set in `ACME_EMAIL`

### Custom Domains

Each project can have custom domains configured. GoForge validates DNS records and tracks SSL certificate status. See [Custom Domains Guide](../guides/custom-domains.md).

## Domain Validation

GoForge validates domain names against these rules:

- Must be a valid domain format (e.g., `app.example.com`)
- Cannot be an IP address
- Cannot use `localhost` or `.localhost` domains
- Must have proper DNS records pointing to your server

## Troubleshooting

### Certificate not issued

```bash
# Check Traefik logs
docker compose logs traefik | grep -i acme

# Verify DNS
dig +short your-domain.com
nslookup your-domain.com
```

### HTTP-01 challenge failing

Ensure port 80 is open and accessible from the internet. Let's Encrypt must be able to reach `http://your-domain.com/.well-known/acme-challenge/`.

### Certificate storage

Certificates are stored in `/letsencrypt/acme.json` inside the Traefik container (mapped to the `traefik_letsencrypt` Docker volume).
