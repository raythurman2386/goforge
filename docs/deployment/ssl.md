# SSL & Certificates

GoForge uses Traefik for automatic SSL certificate management via Let's Encrypt.

## How SSL Works in GoForge

```
Client (HTTPS) -> Traefik (SSL termination) -> GoForge (HTTP internally)
```

Traefik handles:

1. Receiving HTTPS connections on port 443
2. Automatic certificate provisioning via Let's Encrypt ACME
3. Certificate renewal (before expiration)
4. HTTP-to-HTTPS redirect

## Configuration

### Required Settings

```bash
# Email for Let's Encrypt registration (required)
ACME_EMAIL=admin@example.com

# Your domain
GOFORGE_DOMAIN=goforge.example.com
```

### DNS Requirements

Your domain must have an A record pointing to your server's public IP:

```
goforge.example.com    A    203.0.113.50
```

### Port Requirements

Ports 80 and 443 must be accessible from the internet:

- Port 80: Required for Let's Encrypt HTTP-01 challenge
- Port 443: HTTPS traffic

## Certificate Storage

Certificates are stored in the `traefik_letsencrypt` Docker volume at `/letsencrypt/acme.json`.

### Backing Up Certificates

```bash
docker run --rm -v goforge_traefik_letsencrypt:/data -v $(pwd):/backup \
  alpine cp /data/acme.json /backup/acme.json
```

### Restoring Certificates

```bash
docker run --rm -v goforge_traefik_letsencrypt:/data -v $(pwd):/backup \
  alpine cp /backup/acme.json /data/acme.json
```

## Deployed Application SSL

Each application deployed through GoForge gets its own SSL certificate:

1. Project is configured with a domain (e.g., `myapp.example.com`)
2. GoForge creates a container with Traefik labels
3. Traefik detects the container and its routing rules
4. Traefik provisions a Let's Encrypt certificate for the domain
5. Traffic is routed: Internet -> Traefik (HTTPS) -> Container (HTTP)

## Wildcard Certificates

Traefik supports wildcard certificates via DNS-01 challenge, but this requires additional DNS provider configuration. GoForge uses HTTP-01 challenge by default, which provisions individual certificates per domain.

## Cloudflare SSL

When using Cloudflare Tunnel (`CLOUDFLARE_PROXY_MODE=true`), SSL is handled by Cloudflare instead of Traefik. See [Cloudflare Tunnel Configuration](../configuration/cloudflare.md).

## Troubleshooting

### Rate Limits

Let's Encrypt has rate limits:

- 50 certificates per registered domain per week
- 5 duplicate certificates per week
- 300 new orders per account per 3 hours

If you hit rate limits during testing, use the Let's Encrypt staging environment.

### Certificate Not Issued

```bash
# Check Traefik logs for ACME errors
docker compose logs traefik | grep -i acme

# Verify DNS
dig +short your-domain.com

# Verify port 80 is accessible
curl -I http://your-domain.com
```
