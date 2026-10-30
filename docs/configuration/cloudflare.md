# Cloudflare Tunnel Configuration

GoForge supports Cloudflare Tunnel for secure ingress without exposing ports on your server. This is an alternative to direct port exposure with Let's Encrypt.

## How It Works

Instead of Traefik handling SSL directly, Cloudflare Tunnel creates an encrypted connection from your server to Cloudflare's edge network:

```
Internet -> Cloudflare Edge (SSL) -> Cloudflare Tunnel -> Traefik (HTTP) -> GoForge
```

**Benefits:**

- No need to expose ports 80/443 on your server
- Cloudflare handles DDoS protection, SSL, and CDN
- No Let's Encrypt configuration needed
- Works behind NAT/firewalls

## Setup

### Step 1: Create a Cloudflare Tunnel

1. Go to [Cloudflare Zero Trust Dashboard](https://one.dash.cloudflare.com/)
2. Navigate to **Networks** > **Tunnels**
3. Click **Create a tunnel**
4. Choose **Cloudflared** connector
5. Name your tunnel (e.g., "goforge")
6. Copy the tunnel token

### Step 2: Configure Environment

```bash
# Enable Cloudflare proxy mode
CLOUDFLARE_PROXY_MODE=true

# Set your tunnel token
CLOUDFLARE_TUNNEL_TOKEN=eyJhIjoiYWN...your-token-here
```

### Step 3: Start with Cloudflare Profile

```bash
docker compose --profile cloudflare up -d
```

This starts the `cloudflared` container alongside the other services.

## What Changes in Proxy Mode

When `CLOUDFLARE_PROXY_MODE=true`:

| Setting | Standard Mode | Cloudflare Mode |
|---------|--------------|-----------------|
| Traefik entrypoint | `websecure` (HTTPS, port 443) | `web` (HTTP, port 80) |
| SSL certificates | Let's Encrypt (ACME) | Not needed (Cloudflare handles SSL) |
| HTTP-to-HTTPS redirect | Enabled | Disabled (Cloudflare enforces HTTPS) |
| Port exposure | 80 + 443 required | No ports required |
| Client IP (rate limit, lockout, audit) | Rightmost untrusted `X-Forwarded-For` | `CF-Connecting-IP` from a `TRUSTED_PROXIES` peer |

Traefik forwards `CF-Connecting-IP` exactly as the client sent it. In Cloudflare mode, make sure Traefik is reachable only through the tunnel; a client that reaches it directly can set its own client IP.

## Tunnel Configuration

In the Cloudflare dashboard, configure your tunnel's public hostname:

| Setting | Value |
|---------|-------|
| **Public hostname** | `goforge.example.com` |
| **Service type** | HTTP |
| **Service URL** | `traefik:80` |

For each deployed application, add additional public hostnames pointing to `traefik:80`.

## Service Domains

One-click services (Gitea, Vaultwarden, and others) need a public host in the **Domain** field. In proxy mode the configure form leaves Domain empty, because a `<name>.localhost` host cannot be reached through the tunnel.

- Use a first-level host such as `vaultwarden-app.example.com`. Cloudflare Universal SSL covers `*.example.com` but not `*.apps.example.com`, so a second-level name fails the TLS handshake.
- Before you deploy, create a proxied CNAME for the host that points at `<tunnel-id>.cfargotunnel.com`. A proxied wildcard CNAME on the zone that already points at the tunnel also works.

See [Public HTTPS behind Cloudflare Tunnel](../guides/services.md#public-https-behind-cloudflare-tunnel) for how templates use the domain.

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────┐
│   Cloudflare    │     │   Your Server    │     │             │
│   Edge Network  │◄───►│   cloudflared    │◄───►│   Traefik   │◄──► GoForge
│   (SSL/CDN)     │     │   (tunnel)       │     │   (HTTP)    │◄──► Apps
└─────────────────┘     └──────────────────┘     └─────────────┘
```

## Considerations

!!! note "Mixed mode"
    You can use Cloudflare Tunnel for GoForge itself while still using direct Traefik routing for deployed applications, but this requires additional Cloudflare configuration for each application domain.

!!! warning "Tunnel reliability"
    If the Cloudflare Tunnel goes down, all traffic is blocked. Ensure you have monitoring in place for the `cloudflared` container. Consider keeping SSH access available via a separate path.
