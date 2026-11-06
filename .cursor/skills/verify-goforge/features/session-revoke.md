# Session revoke on password change

A signed-in local account changes its password in Settings → **Change Password** (`PUT /settings/password`). On success GoForge deletes every other session of that account and keeps the browser that made the change, so another browser (or a stolen `goforge_session` cookie) is sent to `/login` on its next request. The toast says how many were ended (`Password updated. Signed out 2 other sessions.`). Settings → **Sessions** → **Sign out other sessions** (`POST /settings/sessions/revoke-others`) does the same without a password change, for local and GitHub accounts. Both write one audit row, `user.password.change` or `user.sessions.revoke`, with `resource_type` `user`, `details.sessions_revoked` (count), and `details.via` = `ui`. API tokens are not sessions and keep working (v0.5.0 S2, t1861u).

## Sub-features

- `revoke-on-change` after jar A changes the password, jars B and C get `303` with `Location: /login` on `GET /dashboard`.
- `keep-current` jar A still gets `200` on `GET /dashboard` after the change.
- `revoke-count` `sessions` rows for the user go from 3 to 1, and the toast and audit row both say `2`.
- `failed-change-keeps-sessions` a wrong current password returns `400` and ends no session (handler test).
- `other-accounts-untouched` another account's sessions survive (repository and handler tests).
- `old-password-refused` login with the old password returns `401`; the new password logs in (`303` to `/dashboard`).
- `sign-out-others` the Profile **Sessions** card button ends every other session (`Signed out 1 other session.`), keeps jar A, and writes `user.sessions.revoke`.
- `audit-no-secret` the two actions carry only `sessions_revoked` and `via` in `details`.
- `csrf` `POST /settings/sessions/revoke-others` without a CSRF token is `403`.

## How to get to it (user POV)

- Sign in with a local account in two browsers. In one, open **Settings → Security**, fill **Change Password**, and press **Update Password**. Reload the other browser: it lands on the sign-in page.
- Or press **Sign out other sessions** in the **Sessions** card (it asks for confirmation first).
- **Settings → Activity** lists the `user.password.change` or `user.sessions.revoke` row.

## Driving it with verify.sh

Host mode (live stack holds `:8080`; see SKILL.md "Host mode"): throwaway `postgres:17-alpine` on a free loopback port, `goforge serve` on `127.0.0.1:18080` with `GOFORGE_ENABLE_REGISTRATION=true`, and `GOFORGE_VERIFY_PSQL` pointing at that database.

- **Three sessions.** Register in jar A (`POST /auth/register`), then log in with the same account in jars B and C (`GET /login` for `goforge_csrf`, then `POST /auth/login`). Each is `303` to `/dashboard`. `verify.sh psql "select count(*) from sessions where user_id='<id>'"` prints `3`, and `GET /dashboard` is `200` in all three jars.
- **Change in jar A.** `curl -b jar-a -c jar-a -X PUT /settings/password -H 'HX-Request: true' --data-urlencode csrf_token=<jar A goforge_csrf> --data-urlencode current_password@old --data-urlencode new_password@new --data-urlencode confirm_password@new` is `200` with `Password updated. Signed out 2 other sessions.`
- **Result.** Session count `1`. `GET /dashboard` is `200` in jar A and `303` `Location: /login` in jars B and C.
- **Audit.** `verify.sh psql "select action, details->>'sessions_revoked', details->>'via' from audit_logs where action like 'user.%' order by created_at"` shows `user.password.change|2|ui`. Keep the word `password` out of the SQL text: `verify.sh psql` refuses queries that look like they read secrets, so use `like 'user.%'`.
- **Old password.** `POST /auth/login` with the old password is `401`; with the new one in jar D it is `303` to `/dashboard`.
- **Sign out other sessions.** `curl -b jar-a -c jar-a -X POST /settings/sessions/revoke-others -H 'HX-Request: true' --data-urlencode csrf_token=<...>` is `200` with `Signed out 1 other session.`; jar D then gets `303` to `/login`, jar A `200`, and a `user.sessions.revoke|1|ui` row is written.
- **Proof.** One verdict file in the evidence directory with each status, the session counts before and after, and the audit rows. Never copy the jars or password files into it.

Unit coverage: `internal/database/repositories/session_test.go` `TestSessionRepository_DeleteByUserIDExcept` (keeps the named session, removes the rest, leaves other users alone, empty keep id removes all) and `internal/web/handlers/settings_sessions_test.go` (`TestSettingsHandler_UpdatePassword_RevokesOtherSessions` with two real sessions plus another account's, a failed change first, then the audit row; `TestSettingsHandler_RevokeOtherSessions` with a fake store). Both DB tests skip without `DATABASE_URL`.

## Gotchas

- The current session is the one on the request context (`auth.SessionContextKey`). A request authenticated by a full-scope API token on the UI routes has no session, so a password change made that way ends every session of the account.
- API tokens and `/mcp` bearer auth are not revoked by either action. Revoke tokens in Settings → Security → API tokens.
- If the password is saved but the session delete fails, the handler answers `500` with `Password updated, but other sessions could not be signed out` and writes `user.password.change` with `revoke_error: true` and `sessions_revoked: 0`. Use **Sign out other sessions** to retry.
- A `goforge backup restore` clears every session separately (see [backup-restore.md](./backup-restore.md)).
- Never drive the live `goforge-goforge-1` on `:8080` for this; use a stack this run started.
