# Per-project and per-service API token scoping

On Settings → Profile, the token and MCP token forms have **Restrict to (optional)** with the owner's projects and services. A token with nothing checked is host-wide, as before. A token with some checked reaches only those projects (with their environments, deployments, and containers) and those services on `/api/v1` and `/mcp`; anything else answers `403 resource_out_of_scope` before any dry run, audit row, or Docker call. Scopes and the `protected` rule still apply. `whoami` shows the lists and the token list shows the restriction.

## Sub-features

- `scope-create-restricted`: `POST /settings/tokens` with `name`, `scopes=read&scopes=write`, `project_ids=$PID`, and `service_ids=$SID` (both owned) answers `201` with `id="created-token"`; `select project_ids, service_ids from api_tokens where name = '<name>'` is `{$PID}` and `{$SID}`; the `api_token.create` row has `details.project_ids` and `details.service_ids`.
- `scope-create-refusals`: the same form with another user's or a missing ID, a non-UUID, `scopes=full`, or no scope is `400` and stores nothing.
- `scope-host-wide-default`: a token created without lists stores `{}` and `{}`, reaches every project and service, and `whoami` shows `"restricted": false`.
- `scope-in-list`: with the restricted token, `GET /api/v1/projects/$PID`, `GET /api/v1/services/$SID`, and a dry-run `POST /api/v1/services/$SID/restart` are `200`.
- `scope-out-of-list`: with the restricted token, `GET /api/v1/projects/$OTHER_PID`, `GET /api/v1/services/$OTHER_SID`, a dry-run restart of `$OTHER_SID`, `GET /api/v1/dashboard/stats`, `GET /api/v1/git-sources`, `POST /api/v1/services`, and `GET /api/v1/audit-log` without `resource_id` are each `403` `resource_out_of_scope`, and no `api_v1.*` audit row is written for them.
- `scope-lists-filtered`: `GET /api/v1/projects`, `/services`, and `/containers` return only listed items.
- `scope-whoami`: `GET /api/v1/whoami` shows `token.restricted: true`, `token.project_ids`, `token.service_ids`; `protected_services` lists only listed services.
- `scope-mcp`: `POST /mcp` `tools/call` `whoami` shows the same lists, and `get_service` on `$OTHER_SID` is a tool error carrying `resource_out_of_scope`.
- `scope-token-list`: `GET /settings/tokens` shows `data-token-restricted` with `data-token-project="$PID"` and `data-token-service="$SID"` badges for the restricted token, and `data-token-host-wide` for the others.
- `scope-ui-routes`: a restricted token (even with every scope) is `401` on UI routes such as `/dashboard`.

## How to get to it (user POV)

- Settings → Profile → API Tokens → **Generate Token** (or MCP → **Generate MCP token**). Pick scopes, then check projects or services under **Restrict to (optional)**. The fieldset is shown only when the account has a project or service.
- The token list shows "Restricted to" with one badge per project or service, or "All projects and services".
- There is no edit form. Revoke and create a new token to change the lists; **Rotate** keeps them.

## Driving it with verify.sh

- Preconditions: `drive-register`, `drive-login`, a project this run created, and a service row this run created (a SQL insert is enough for reads and dry runs; no Docker needed). The CSRF header comes from the jar as in the other UI drives.
- Create: `curl -b $JAR -H 'HX-Request: true' -H "X-CSRF-Token: $CSRF" -d "name=scoped&scopes=read&scopes=write&project_ids=$PID&service_ids=$SID" $BASE/settings/tokens`, extract `id="created-token"` into a 0600 header file, never echo it.
- API: `curl -H @token-scoped-header $BASE/api/v1/whoami` and the routes above; compare `.error.code`.
- No `verify.sh` subcommand drives this yet; t1668u drove it in host mode on minis against a throwaway Postgres and a missing Docker socket (`~/gf1668u/drive.sh`).

## Gotchas

- Ownership comes first: another user's project is still `404` and another user's service or container `403`; only owned-but-unlisted resources are `resource_out_of_scope`.
- Linking or unlinking a service to a project needs both on the lists.
- A deployment container is in scope through its deployment's project; a service container through its service. An untracked or unresolvable container is out of scope.
- `GET /api/v1/health`, `whoami`, and `service-templates` stay open to restricted tokens (no per-user data).
- Tests: `TestRestrictedTokenOutOfScope`, `TestRestrictedTokenInScope`, `TestRestrictedTokenListsAreFiltered`, `TestWhoamiReportsRestriction`, `TestContainerInScopeByDeployment` (apiv1), `TestAPITokenResourceScope`, `TestNormalizeResourceIDs` (models), `TestAPITokenRepository_ResourceScope` (repositories, needs `DATABASE_URL`), `TestCreateAPIToken_Restricted*`, `TestCreateAPIToken_HostWideByDefault`, `TestListAPITokens_ShowsRestriction`, `TestNewAPITokenModal_OffersOwnedResources` (handlers), `TestAPITokenMiddleware_Scopes` (auth).
