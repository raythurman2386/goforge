# One-click service credentials

A one-click Postgres service stores its config and connection info sealed, and the service page still shows a connection string the user can read.

## Sub-features

- `service-create` starts Postgres from the catalog form.
- `service-sealed` stores `config` and `connection_info` with the `_goforge_enc` marker and without the plaintext password.
- `service-ui` shows the connection string on the instance page under `Connection Details`.

## How to get to it (user POV)

- In the sidebar, choose `Services`.
- On the `PostgreSQL` card, choose `Deploy`. The form heading is `Configure PostgreSQL`.
- Submit `Deploy Service`.
- Open the new instance from `Your Services`. The page shows `Connection Details`.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes` and `drive_docker=yes`.
- `jar-a.txt` is the bootstrap session.
- `/tmp/goforge-verify/state/service.password` is mode `600` and holds the password to submit. Create it with `openssl rand -hex 16`. Do not print the file.

- **Open the form.** `GET /services/postgres/configure` with `jar-a.txt` returns `200` and the body contains `Configure PostgreSQL`.
- **Deploy.** Post the catalog form. `name` is `verify-pg`. `database` is `app`. `username` is `postgres`. `password` is the file contents. `csrf_token` is the cookie value.

```bash
curl -sS -D /tmp/goforge-verify/state/service.headers -o /dev/null \
  -b /tmp/goforge-verify/state/jar-a.txt -c /tmp/goforge-verify/state/jar-a.txt \
  -X POST http://127.0.0.1:8080/services/postgres \
  --data-urlencode "csrf_token=$csrf" \
  --data-urlencode "name=verify-pg" \
  --data-urlencode "database=app" \
  --data-urlencode "username=postgres" \
  --data-urlencode "password@/tmp/goforge-verify/state/service.password"
```

The response is `303` and `Location` is `/services`.

- **Sealed row.** After the instance exists, run `.cursor/skills/verify-goforge/scripts/verify.sh check-sealed verify-pg`. Append its two lines to `/tmp/goforge-verify/proof/service-verdict.txt`. `sealed_marker=yes` and `plaintext_password_present=no` are the pass. The command deletes its row dump.
- **UI.** Read the instance id with `verify.sh psql "select id from service_instances where name = 'verify-pg'"`. `GET /services/$INSTANCE_ID` returns `200` and the body contains `Connection Details` and `internal`. Record those two booleans. Do not save the HTML. It contains the connection string.
- **Proof.** `service-verdict.txt` has the HTTP status, the sealed booleans, and the UI booleans. It does not have the password or the raw JSON.

## Gotchas

- Not re-driven live since #28. The expectations below come from the merged source.
- The Postgres template marks `password` with `generate: true`. The server generates a password only when the submitted value is empty, so the password file this drive submits is the one stored, and `check-sealed` can test that its plaintext is absent.
- `config` and `connection_info` are written as `{"_goforge_enc":"..."}` with `ENCRYPTION_KEY`. Legacy plaintext rows still read back as-is.
- The UI decrypts the same JSON and prints it. Seeing `internal` on the page does not prove the row is sealed.
- `check-sealed` output is safe to keep. The row dump under `/tmp/goforge-verify/state` is not. Cleanup removes that directory.
- Instance deploy runs in a background goroutine. Poll `verify.sh psql "select status from service_instances where name = 'verify-pg'"` until it leaves `pending` before calling the row a finished credential write. Connection info is filled when the status moves to `running`.
