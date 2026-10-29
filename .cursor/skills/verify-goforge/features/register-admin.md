# Register the bootstrap account

Register lets the first person create an account and land on the dashboard already signed in. Registration then closes for everyone else. The account whose email matches `GOFORGE_ADMIN_EMAIL`, ignoring case, gets the `admin` role. Every other account is `user`.

## Sub-features

- `register-open` shows the registration form when the `users` table is empty.
- `register-submit` creates that account and opens the dashboard with a session cookie.
- `register-admin-role` stores `role=admin` for an email that matches `GOFORGE_ADMIN_EMAIL` with different case. The email is stored lowercase.
- `register-closed` returns `403` for `GET /auth/register` after the first account exists.
- `register-closed-post` returns `403` for a second account's `POST /auth/register` from a fresh session.

## How to get to it (user POV)

- Open `http://127.0.0.1:8080/login` and follow `Don't have an account? Sign up`. The link is shown only while registration is allowed.
- Open `http://127.0.0.1:8080/auth/register` directly.
- Submit the form whose button reads `Register`.

## Driving it with verify.sh

Preconditions:

- Launch used the default `GOFORGE_ENABLE_REGISTRATION=false` and `GOFORGE_ADMIN_EMAIL=verify-admin@example.test`.
- `verify.sh doctor` reports `drive_auth=yes`.
- `verify.sh psql "select count(*) from users"` prints `0`.

- **Open the form.** Run `.cursor/skills/verify-goforge/scripts/verify.sh drive-register`. Before the POST, `GET /auth/register` returns `200` and the body contains `Create your account`.
- **Submit the account.** The helper posts `email=Verify-Admin@Example.Test` plus a random 32-character password and the matching `confirm_password`, with `csrf_token` taken from the `goforge_csrf` cookie. The POST returns `303` and `Location` ends in `/dashboard`.
- **Confirm the session.** `GET /dashboard` with the `goforge_session` cookie returns `200` and the body contains `Dashboard`. `users` has one row.
- **Admin grant.** `select email, role from users` prints `verify-admin@example.test` and `admin`.
- **Registration closes.** `GET /auth/register` with the same session returns `403`. A fresh session posting `verify-other@example.test` returns `403`, and `users` still has one row.
- **Proof.** The helper writes `/tmp/goforge-verify/proof/register-verdict.txt` with each sub-feature set to `pass` or `fail`. The file has no cookie and no password.

## Gotchas

- The route is `/auth/register`, not `/register`.
- Password length must be 8 to 256 characters. A mismatch returns `400` and the form error `Passwords do not match`.
- The CSRF cookie is not HttpOnly. The session cookie is HttpOnly. Both must stay out of the evidence directory.
- Registration is allowed while `users` is empty, even with `GOFORGE_ENABLE_REGISTRATION=false`. A failed user count falls back to the flag, so it does not open registration by itself.
- With `GOFORGE_ADMIN_EMAIL` empty, the first account is a `user` and nobody is admin. Set the variable and restart to promote an existing account.
- Emails are stored lowercase, and a unique index on `lower(email)` rejects a second signup that differs only by case.
- A second drive against the same volume finds `users` not empty and exits `2`. Run cleanup and launch a fresh volume before repeating.
