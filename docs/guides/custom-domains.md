# Custom Domains

GoForge supports custom domains for your deployed projects, with automatic SSL certificate provisioning via Traefik and Let's Encrypt.

## Adding a Custom Domain

1. Navigate to your project's **Settings**
2. Go to the **Domains** section
3. Enter your custom domain (e.g., `app.example.com`)
4. Click **Add Domain**

## DNS Configuration

After adding a domain, configure your DNS records:

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

## DNS Verification

GoForge verifies that your domain's DNS records are correctly configured by checking for TXT records. The verification process:

1. GoForge provides a TXT record value to add to your DNS
2. Add the TXT record at your DNS provider
3. GoForge periodically checks for the record
4. Once verified, traffic routing is enabled

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

## SSL Status

Each custom domain has an SSL status:

| Status | Description |
|--------|-------------|
| `none` | No SSL certificate |
| `pending` | Certificate is being provisioned |
| `active` | Certificate is valid and active |
| `expiring` | Certificate is expiring soon |
| `expired` | Certificate has expired |
| `failed` | Certificate provisioning failed |

## Domain Validation Rules

GoForge validates domain names against these rules:

- Must be a valid domain format
- Cannot be an IP address
- Cannot be `localhost` or a `.localhost` subdomain
- Must be a valid domain format (validated via `isValidDomainFormat`)

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
