# Two-factor authentication (TOTP)

A local password account turns on TOTP in Settings → Security → **Two-factor authentication**: `POST /settings/2fa/setup` checks the current password and returns a dialog with a QR code (`data-testid="two-factor-qr"`, a `data:image/png` URI of `otpauth://totp/GoForge:<email>?secret=…&issuer=GoForge`) and the setup key (`#two-factor-secret`). `POST /settings/2fa/enable` with the first `code` turns it on and returns the recovery codes dialog (`#recovery-codes`, 10 codes `xxxxx-xxxxx`, shown once). From then on `POST /auth/login` with the right password answers `303 /auth/2fa` and sets the `goforge_2fa` cookie (HttpOnly, 5 minutes) instead of `goforge_session`; `POST /auth/2fa` with `code` (TOTP or recovery code) creates the session. `POST /settings/2fa/disable` needs `password` and `code`. `goforge user reset-2fa <email>` clears it from the host. GitHub sign-in never asks for the code, and API tokens and `/mcp` are unaffected (v0.5.0 S4, t1873u).

## Sub-features

- `enroll` setup with the wrong password is `400`; with the right one the dialog carries the QR and key; a wrong first code is `400` and leaves 2FA off; the right one shows 10 recovery codes and writes `user.2fa.enable` with `recovery_codes` = 10.
- `login-needs-code` with 2FA on, a right password gives `303` `Location: /auth/2fa`, a `goforge_2fa` cookie, and no `goforge_session`; `GET /dashboard` with that jar is `303` to `/login`.
- `no-code` `POST /auth/2fa` with an empty or wrong code is `401` (`Invalid authentication code`) and writes `login.2fa_failed`.
- `totp-login` a current TOTP code gives `303 /dashboard` and a `login` row with `second_factor` = `totp`. The same code again (new challenge, same 30 s step) is refused.
- `recovery-once` a recovery code gives `303 /dashboard` (`second_factor` = `recovery_code`); the same code on a later challenge is `401`.
- `challenge-cap` after 5 wrong codes the challenge ends: `401` with `Too many wrong codes…`, the cookie is cleared, and a right code with the old cookie goes back to `/login`.
- `throttle` wrong codes count toward the login lockout; 10 failures for the account or IP in 15 minutes make `/auth/2fa` and `/auth/login` answer `429` with `Retry-After`.
- `expiry` a challenge older than 5 minutes is refused (unit and handler tests with a fake clock).
- `bearer-unaffected` with 2FA on, an API token still gets `200` on `GET /api/v1/whoami` and `/mcp` `tools/list` with no code.
- `disable` password plus a code turns it off (`user.2fa.disable` with `second_factor`), and the next password login goes straight to `/dashboard`.
- `cli-reset` `goforge user reset-2fa <email>` prints `Two-factor authentication is off for …`, deletes the key, recovery codes, and challenges, and writes `user.2fa.reset` with `via` = `cli` (Activity source **CLI**). A second run prints `nothing changed` and writes nothing.
- `github-skips` a GitHub callback for an account with 2FA on creates the session directly (handler test with a fake GitHub provider). A GitHub-only account (no password) sees a note instead of **Set up 2FA**, and setup answers `400`.

## How to get to it (user POV)

- **Settings → Security → Two-factor authentication** → enter the password → **Set up 2FA** → scan the QR code → enter the code → **Turn on 2FA** → save the recovery codes → **Done**.
- Sign out, sign in with the password, then type the code from the app (or a recovery code) on the **Two-factor authentication** page.
- **Settings → Activity** lists `user.2fa.enable`, `login.2fa_failed`, `login`, `user.2fa.disable`, and `user.2fa.reset`.

## Driving it with verify.sh

Host mode (live stack holds `:8080`; see SKILL.md "Host mode"): throwaway `postgres:17` on a free loopback port, `goforge serve` on `127.0.0.1:18080` with `GOFORGE_ENABLE_REGISTRATION=true` and an `ENCRYPTION_KEY` (the key is sealed with it), and `GOFORGE_VERIFY_PSQL` pointing at that database.

- **Register** in jar A (`GET /auth/register` for `goforge_csrf`, then `POST /auth/register`), `303` to `/dashboard`.
- **Enroll.** `curl -b jar-a -X POST /settings/2fa/setup -H 'HX-Request: true' --data-urlencode csrf_token=<…> --data-urlencode password@pw` is `200`; pull the key out of `<code id="two-factor-secret">` into a mode-600 file (strip spaces). Never print it. Compute the code locally (Python `hmac`/`hashlib`/`struct`, RFC 6238 SHA-1, 30 s) and `POST /settings/2fa/enable` with `code`; save the 10 codes from `#recovery-codes` to a mode-600 file. Check `verify.sh psql "select enabled_at is not null, (select count(*) from user_recovery_codes where used_at is null) from user_totp"` prints `t|10`.
- **Sign out** (`POST /auth/logout`).
- **Password only.** Fresh jar B: `POST /auth/login` → `303` `Location: /auth/2fa`, jar has `goforge_2fa` and no `goforge_session`. `GET /dashboard` → `303 /login`. `POST /auth/2fa` with an empty code and with `000000` → `401`.
- **TOTP.** `POST /auth/2fa` with the computed code → `303 /dashboard`, then `GET /dashboard` → `200`. Wait for the next 30 s step before the next TOTP login, or the step guard refuses it (by design).
- **Recovery code.** New jar C: password, then a recovery code → `303 /dashboard`. Jar D: password, the same recovery code → `401`.
- **Throttle.** Keep posting `000000` on fresh challenges (password, then 5 wrong codes, twice); the next request is `429` with `Retry-After`. Count the wrong codes and passwords you sent so the number makes sense (the account counter keeps the earlier `401`s within 15 minutes).
- **Bearer unaffected.** Before the throttle step (it locks the account for UI sign-in only), create a token in jar A (`POST /settings/tokens` with `name`), then `curl -H 'Authorization: Bearer <token>' /api/v1/whoami` → `200` and `/mcp` `initialize` + `tools/list` → `200` with no code. Never print the token.
- **Disable and CLI reset.** `POST /settings/2fa/disable` with `password` and a code → `200` toast; enable again; then `goforge user reset-2fa <email>` with the same env → `Two-factor authentication is off`; rerun → `nothing changed`.
- **Audit.** `verify.sh psql "select action, details->>'second_factor', details->>'via' from audit_logs where action like 'login%' or action like 'user.2fa%' order by created_at"`. Keep the words `secret`, `token`, and `password` out of the SQL (verify.sh refuses them).
- **Proof.** One verdict file with each status and the audit rows. Never copy jars, the key, codes, or tokens into it.

Unit coverage: `internal/auth/totp_test.go` (RFC 6238 vectors, ±1 step skew, replay, recovery code shape and normalization), `internal/auth/twofactor_test.go` (enroll confirm, wrong first code, recovery code single use, challenge expiry with a fake clock, 5-attempt cap, single use, disable, reset), `internal/web/handlers/two_factor_test.go` (DB-backed: enroll → login without and with a code → recovery once → disable; challenge expiry; wrong codes reach `429`; GitHub callback creates a session with no TOTP; GitHub-only account cannot enroll), `cmd/goforge/user_test.go` (`TestReset2FA`), `internal/web/templates/pages/settings/two_factor_test.go`, and the CLI source in `audit_log_activity_test.go` and `activity_test.go`. DB tests skip without `DATABASE_URL`.

## Gotchas

- With 2FA on, a right password does not call the throttle's `Success`, so the account counter is cleared only by a right code.
- The TOTP step guard means one code works once. Two logins in the same 30 s step need a recovery code or a wait.
- `ENCRYPTION_KEY` must stay the same; with a different key the stored TOTP key cannot be opened and sign-in fails closed (`500`) until `goforge user reset-2fa` runs.
- GitHub sign-in links a local account with the same email and skips the TOTP step by design (Raymond's lock). Someone who controls a GitHub account with that email gets in without the code; the docs tell users to turn on GitHub 2FA.
- Never drive the live `goforge-goforge-1` on `:8080` for this; use a stack this run started.
