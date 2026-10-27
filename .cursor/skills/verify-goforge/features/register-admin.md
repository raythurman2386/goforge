# Register the bootstrap account

Register lets the first person create an account, land on the dashboard already signed in, and leaves registration closed for everyone else.

## Sub-features

- `register-open` shows the registration form when the `users` table is empty.
- `register-submit` creates that account and opens the dashboard with a session cookie.
- `register-closed` rejects another registration after the first account exists.

## How to get to it (user POV)

- Open `http://127.0.0.1:8080/login` and follow `Don't have an account? Sign up`.
- Open `http://127.0.0.1:8080/auth/register` directly.
- Submit the form whose button reads `Register`.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes`.
- `verify.sh psql "select count(*) from users"` prints `0`.
- No cookie jar exists yet for this run.

- **Open the form.** Visit the register page. Run `.cursor/skills/verify-goforge/scripts/verify.sh drive-register`. Before the POST, `GET /auth/register` returns `200` and the body contains `Create your account`.
- **Submit the account.** The helper posts `email=verify-admin@example.test` plus a password of at least 8 characters and the matching `confirm_password`, with `csrf_token` taken from the `goforge_csrf` cookie. The POST returns `303` and `Location` ends in `/dashboard`.
- **Confirm the session.** `GET /dashboard` with the `goforge_session` cookie returns `200` and the body contains the heading `Dashboard`. `verify.sh psql "select count(*) from users"` prints `1`.
- **Registration closes.** `GET /auth/register` after that account exists returns `403`.
- **Proof.** The helper writes `artifacts/verify-goforge-proof/register-verdict.txt` with `register-open`, `register-submit`, and `register-closed` set to `pass` or `fail`. The file has no cookie and no password.

## Gotchas

- The route is `/auth/register`, not `/register`.
- Password length must be 8 to 256 characters. A mismatch returns `400` and the form error `Passwords do not match`.
- The CSRF cookie is not HttpOnly. The session cookie is HttpOnly. Both must stay out of the evidence directory.
- This tree does not close registration when the first user appears. `GOFORGE_ENABLE_REGISTRATION` defaults to true and `RegisterPage` checks only that flag. `register-closed` is `fail` while `GET /auth/register` stays `200`. Record the status. Do not change the expected `403`.
- There is no admin role. The bootstrap account is the first row in `users`.
- A second drive against the same volume returns `400` and `User already exists`. Launch a fresh volume before repeating `register-submit`.
