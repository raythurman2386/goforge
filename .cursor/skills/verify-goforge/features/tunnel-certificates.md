# Certificate scope in Tunnel and direct modes
Public TLS handled by Cloudflare in Tunnel mode versus direct Traefik routing, without an unbuilt GoForge certificate dashboard.

## Sub-features
- Recommended Tunnel mode: Cloudflare terminates TLS; there is no certificate primary-navigation item, renewal action, or certificate API.
- Direct mode: routing retains Let's Encrypt; documentation explicitly states that certificate status/expiration and renewal UI remain unbuilt.
- `CLOUDFLARE_PROXY_MODE` is read as configuration, not runtime `cloudflared` container detection. Mixed mode remains possible and requires an explicit routing decision.
- A future certificate UI for direct installations must show that Tunnel mode has no Traefik certificate to display; its checks must run in both configurations before exposing status or renewal controls.

## How to get to it (user POV)
1. Prepare separate throwaway stacks or labeled host serves, set `CLOUDFLARE_PROXY_MODE=true` for the Tunnel run and `false` for the direct run. Record the exact source revision; do not stop a live stack.
2. Navigate Dashboard → Projects → a project's Settings → Services → a service's Configure page. Confirm no primary-nav certificate item or renewal control exists.
3. In Tunnel mode confirm one-click public-host guidance points to Cloudflare DNS/Tunnel TLS. In direct mode confirm ordinary HTTPS direct routing guidance remains; use the SSL guide for the explicit missing-UI limitation.
4. Check the SSL and Cloudflare guides for Cloudflare-managed TLS, the direct-install gap, the configuration-not-detection distinction, and mixed mode.
5. Run `go test ./internal/proxy/...` for routing labels: Tunnel omits `traefik.http.routers.*.tls.certresolver`; direct includes it. A future certificate page repeats this drive in both modes and separately verifies its status source and renewal authorization.

## Driving it with verify.sh
Use host mode when port 8080 is occupied, on a free loopback port and throwaway Postgres. Calibrate configuration with non-secret booleans in `/tmp/goforge-verify/proof/slice3/tunnel-direct-verdict.txt`; do not save an environment file, cookie, or password. Proxy tests are labeled checks and are not a claim of a second full stack running at once.

## Gotchas
- No existing certificate route is being removed here: no dashboard exists.
- `.localhost` remains available for direct-mode one-click services; Tunnel deployments need public Cloudflare hostnames.
- `CLOUDFLARE_TUNNEL_TOKEN` is the connector credential, not the application mode signal.
- GoForge can itself use a Tunnel while deployed applications use direct Traefik routing; do not infer container topology from this flag.
