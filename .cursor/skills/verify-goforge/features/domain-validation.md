# Project and environment domain DNS validation

A project owner cannot save a project default or environment override hostname until it resolves to an address. Invalid input gets a format error without a DNS lookup; NXDOMAIN or no address gets an unresolved-domain error; a resolver failure gets a different retryable message. A unified Settings refusal leaves both stored domains unchanged. Blank fields still clear their values, and one-click service local-domain guidance is unchanged.

## Sub-features

- `project-unresolved`: an unresolved **Default Domain** is refused through both `PUT /projects/{id}` and `PUT /projects/{id}/settings`, leaving the stored value unchanged.
- `project-accepted`: a resolvable hostname saves as lowercase with surrounding whitespace and a final dot removed. It remains visible after navigating into Settings, away, and back.
- `environment-unresolved`: an unresolved **Domain** override is refused through the unified Settings form and `PUT /projects/{id}/envs/{envId}`, leaving the stored value unchanged.
- `environment-accepted`: a resolvable override saves and retains precedence over the project default in deployment labels.
- `settings-no-partial-write`: with both fields submitted, a refusal in either field changes neither stored domain.
- `resolver-failure`: a timeout or DNS server error tells the user to check the resolver and retry, not to create a missing record.
- `format-before-dns`: invalid format never invokes the resolver.
- `blank-clears`: blank project or environment fields clear their own value with no lookup.
- `service-localhost-preserved`: from **Services**, Vaultwarden **Configure** still presents its existing `vaultwarden.localhost` value outside Cloudflare Tunnel mode. Postgres is non-web and has no Domain field. Do not submit either form without Docker access.

## How to get to it (user POV)

1. Open **Projects**, choose **New Project**, and create a project.
2. On its **Overview**, open **Settings** from the project navigation.
3. Add a DNS record at your DNS provider, then enter the hostname in **Default Domain** and click **Save Changes**. To use a different hostname for the selected environment, use **Domain** under **Environment Overrides**.
4. Visit **Projects**, return to this project, and open **Settings** again to see the accepted normalized value.
5. For the local-service check, leave Cloudflare mode off and open **Services**, then Vaultwarden **Configure**. You can also open Postgres **Configure** to confirm its non-web template has no Domain field.

## Driving it with verify.sh

Read [login-projects.md](./login-projects.md) first. Run `drive-register` and `drive-login`; creating a project through that helper exercises the actual new-project route. Navigate from the resulting Overview to Settings rather than relying only on a fresh GET. Host mode on port 18080 is expected when the live stack owns 8080; never stop the live stack.

This feature needs a process-local resolver fixture. Do not change system DNS or use public names. One repeatable host-mode option supplies app-only `/etc/hosts` entries `192.0.2.10 app.example.test env.example.test` through the successful drive; the real glibc files resolver and `LookupHost` then return addresses, while unlisted `.example.test` names have none. For the failure drive, restrict the app-only hosts fixture and use a time-limited unusable resolver such as `192.0.2.1` with one-second timeout and one attempt. Record the fixture (not credentials) in evidence. A loopback UDP DNS server and explicitly configured app resolver are also suitable if they serve A records, NXDOMAIN, and resolver failure; do not bind the host-wide DNS port.

Drive the actual form with the session's CSRF token, HTMX request header, `env_id`, `custom_domain`, and `domain`. The standalone environment request also carries `domain`. After every refusal, re-read both project and environment rows with `verify.sh psql` or a second application request. Evidence goes in `/tmp/goforge-verify/proof/domain-validation/`, including the statuses and visible error text, state before and after each mutation, desktop/mobile screenshots for the error and successful return, and redacted app output. Do not save HTML containing CSRF tokens.

For a successful save, click **Overview**, **Projects**, return to the project, then open **Settings** and verify the normalized hostname. Repeat unresolved, accepted, and blank cases for the environment override. Exercise standalone `PUT /projects/{id}/envs/{envId}` with curl as well. Compare the unresolved and resolver-failure error text and confirm no write follows the failure.

Run `verify.sh drive-ui-a11y` against the host-mode instance. Its project Settings scans cover light/dark navigation and the existing accessibility gate. Use one browser drive at a time on the ephemeral app and preserve partial output if the broad route matrix stalls; label its findings by baseline comparison rather than calling them regressions or pre-existing without that comparison. Add a focused Playwright drive for Settings navigation and both DNS outcomes; keep any unrelated findings distinct.

## Gotchas

- A resolvable hostname is not proof that certificates were issued or that the host is reachable over HTTP; this check does neither.
- DNS propagation is outside GoForge's control. A resolver that cannot answer is not the same as a nonexistent hostname.
- Saving valid settings updates the project and environment through two repository writes; no claim of database transaction atomicity is made here.
- Empty values bypass DNS on purpose so environment-over-project precedence and clearing remain available.
- Service **Domain** and **Console Domain** intentionally keep separate `.localhost` and Tunnel rules. Do not add a service-domain DNS call as part of this drive.
