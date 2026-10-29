# Cancel a deployment

Cancel lets the project owner stop an in-flight deployment. The stored status becomes `canceled`, the in-flight pipeline stops, and the status stays `canceled`.

## Sub-features

- `cancel-post` accepts cancel from the deployment page for the owning account.
- `cancel-holds` leaves `deployments.status` equal to `canceled` across a 90 second poll.

## How to get to it (user POV)

- Open a deployment that is still running. The page shows a `Cancel` button when the status is not terminal.
- Confirm the prompt `Are you sure you want to cancel this deployment?`.
- The page returns to that deployment. The status badge reads canceled.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes` and `drive_docker=yes`.
- `jar-a.txt` is the project owner from [deploy-log-replay.md](./deploy-log-replay.md).
- A deployment for `verify-hello` is not terminal. The goforge Dockerfile build stays in `cloning` or `building` long enough to cancel. Read the id with the same SQL as the deploy journey.

- **Cancel.** Read `goforge_csrf` from `jar-a.txt`. Do not print it.

```bash
curl -sS -D /tmp/goforge-verify/state/cancel.headers -o /tmp/goforge-verify/state/cancel.body \
  -b /tmp/goforge-verify/state/jar-a.txt -c /tmp/goforge-verify/state/jar-a.txt \
  -H "X-CSRF-Token: $csrf" \
  -X POST "http://127.0.0.1:8080/projects/$PROJECT_ID/deployments/$DEPLOYMENT_ID/cancel"
```

The response is `200` and `HX-Redirect` is `/projects/$PROJECT_ID/deployments/$DEPLOYMENT_ID`.

- **Poll.** Every 5 seconds for 90 seconds, run `verify.sh psql "select status from deployments where id = '$DEPLOYMENT_ID'"`. Append each line to `/tmp/goforge-verify/proof/cancel-verdict.txt`.
- **Proof.** Every polled status is `canceled`. A later row of `failed`, `running`, or `cancelled` is `cancel-holds=fail`.

## Gotchas

- Not re-driven live since #28. The expectations below come from the merged source.
- The status is spelled `canceled`. Migration `015_deployment_status_canceled` rewrites old `cancelled` rows and the check constraint accepts `canceled`. A row of `cancelled` is a fail.
- `canceled` is terminal. Cancel stops the in-flight pipeline and drops a still-pending queue row, so the worker must not move the row to `failed` afterwards. `cancel-holds` fails if it does.
- Cancel on a terminal deployment returns `400` and `deployment is already in a terminal state`. Start a new deployment instead of reusing a finished one.
- Another account's project id returns `404` and `project not found`. A deployment id that belongs to a different project than the one in the path also returns `404`, and cancel does not run.
