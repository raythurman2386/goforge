# Custom Domains

GoForge supports custom domains for your deployed projects, with automatic SSL certificate provisioning via Traefik and Let's Encrypt.

## Adding a Custom Domain

1. Open **Projects**, choose your project, then open its **Settings** tab.
2. Configure the DNS record described below and wait for it to resolve.
3. Enter the hostname in **Default Domain** (for example, `app.example.com`). GoForge saves the lowercase hostname without a trailing dot.
4. To override that default for the selected environment, enter a hostname in **Domain** beneath **Environment Overrides** instead. Leave it blank to use the project's default.
5. Click **Save Changes**.

GoForge requires every nonblank project or environment domain to resolve before it saves either value. An unresolved name produces an error; a resolver timeout or server error asks you to try again instead. If either domain is refused, neither domain changes. Blank fields remain supported for clearing a saved value.

## DNS Configuration

Configure these records **before** entering the domain in project Settings:

### A Record (Direct)

Point your domain directly to your server's IP address:

```
app.example.com    A    203.0.113.50
```

### CNAME Record

Or use a CNAME to point to your server's hostname:

```
app.example.com    CNAME    server.example.com
```

### Cloudflare

If using Cloudflare:

1. Add the DNS record as above
2. Set the proxy status to **Proxied** (orange cloud) for Cloudflare protection
3. Or set to **DNS only** (gray cloud) for direct Traefik SSL

## SSL Certificates

### Automatic (Let's Encrypt)

When not using Cloudflare proxy mode, Traefik automatically provisions Let's Encrypt SSL certificates:

- HTTP-01 challenge verification
- Automatic renewal before expiration
- Certificates stored in the Traefik volume

### With Cloudflare

When `CLOUDFLARE_PROXY_MODE=true`, Cloudflare handles SSL:

- SSL termination at Cloudflare's edge
- No Let's Encrypt certificates needed
- Full SSL mode recommended in Cloudflare settings

## Troubleshooting

### Certificate not issued

1. Verify DNS records are correct: `dig +short your-domain.com`
2. Ensure ports 80 and 443 are accessible from the internet
3. Check Traefik logs: `docker compose logs traefik`
4. Verify `ACME_EMAIL` is set to a valid email

### Domain not resolving

1. DNS propagation can take up to 48 hours (usually much faster)
2. Check with: `nslookup your-domain.com`
3. Verify no conflicting DNS records exist
