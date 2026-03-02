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

## Tunnel Configuration

In the Cloudflare dashboard, configure your tunnel's public hostname:

| Setting | Value |
|---------|-------|
| **Public hostname** | `goforge.example.com` |
| **Service type** | HTTP |
| **Service URL** | `traefik:80` |

For each deployed application, add additional public hostnames pointing to `traefik:80`.

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
