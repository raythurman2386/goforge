# Deploy with log replay

Deploy lets a signed-in user create a project from a public Git URL, start a build, see its real stage and elapsed time, read the same log lines again after the stream reconnects, and follow a rollback result to the restored deployment. The build uses the project's Dockerfile path and build context.

## Sub-features

- `deploy-create` saves a project and its `production` environment.
- `deploy-trigger` starts a deployment from the `Deploy Now` button.
- `deploy-sse-replay` returns the earlier log lines on a second connection to the log stream.
- `deploy-db-rows` shows those lines in `deployment_logs`.
- `deploy-build-context` builds from the submitted build context directory.
- `deploy-stage-elapsed` shows actual model stages and elapsed time in the project banner and deployment detail, without a percentage.
- `deploy-failure-feedback` keeps a terminal error visible on the detail page.
- `rollback-result` names and links the manager-selected restored target after the redirect.
- `rollback-return` preserves the original row's `rolled_back` status and logs on a later return, and the restored target's own status and logs on its detail page.

## How to get to it (user POV)

- In the sidebar, choose `Projects`, then `New Project`. The page heading is `Create Project`.
- Submit `Create Project`.
- On the project page, choose `Deploy Now`.
- Open the deployment. The log pane id is `deployment-logs`. While the deployment is not terminal, that pane connects to the log stream. The `deployment-progress` region names the actual stage and elapsed time. The project banner polls the same stage every three seconds.
- A detail page opened while a deploy is active must remove Cancel and, when the real rollout finishes with manager-eligible status, show Rollback without a reload. Wait for the actual SSE terminal update and check that the page navigation counter has not changed and both button routes target the current deployment. A failed/canceled terminal state never fabricates a Rollback action; reconnect repeats the persisted eligibility.
- After two successful deployments, open the newest and choose `Rollback`. The post-redirect success alert names the restored target and links to it. Follow the link, navigate to the Deployments tab, and return to both rows to check their independent statuses and logs.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes` and `drive_docker=yes`.
- `drive-register` has already created the bootstrap session. Copy `/tmp/goforge-verify/state/jar-login.txt` from `drive-login` to `/tmp/goforge-verify/state/jar-a.txt`, or log in again into that jar.
- The public repository `https://github.com/raythurman2386/goforge.git` is reachable from the `goforge` container.

- **Create the project.** Read `goforge_csrf` from `jar-a.txt` into a shell variable. Do not print it. Post the form.

```bash
curl -sS -D /tmp/goforge-verify/state/project.headers -o /dev/null \
  -b /tmp/goforge-verify/state/jar-a.txt -c /tmp/goforge-verify/state/jar-a.txt \
  -X POST http://127.0.0.1:8080/projects \
  --data-urlencode "csrf_token=$csrf" \
  --data-urlencode "name=verify-hello" \
  --data-urlencode "repository_url=https://github.com/raythurman2386/goforge.git" \
  --data-urlencode "default_branch=main" \
  --data-urlencode "dockerfile_path=Dockerfile" \
  --data-urlencode "build_context=app"
```

The response is `303` and `Location` matches `/projects/` plus an id. `GET` that URL with the same jar returns `200` and the body contains `verify-hello`.

- **Start the deploy.** Read the production environment id with `verify.sh psql "select e.id from environments e join projects p on p.id = e.project_id where p.name = 'verify-hello'"`. Post `Deploy Now`.

```bash
curl -sS -D /tmp/goforge-verify/state/deploy.headers -o /dev/null \
  -b /tmp/goforge-verify/state/jar-a.txt -c /tmp/goforge-verify/state/jar-a.txt \
  -H "X-CSRF-Token: $csrf" \
  -X POST "http://127.0.0.1:8080/projects/$PROJECT_ID/deployments?env_id=$ENV_ID"
```

The response is `200` and the `HX-Redirect` header is `/projects/$PROJECT_ID/deployments`. Read the new deployment id with `verify.sh psql "select d.id from deployments d join environments e on e.id = d.environment_id join projects p on p.id = e.project_id where p.name = 'verify-hello' order by d.created_at desc limit 1"`.

- **First stream.** Capture about 10 seconds, then stop that curl.

```bash
curl -sS -N --max-time 10 \
  -b /tmp/goforge-verify/state/jar-a.txt \
  "http://127.0.0.1:8080/projects/$PROJECT_ID/deployments/$DEPLOYMENT_ID/logs" \
  > /tmp/goforge-verify/proof/deploy-sse-first.txt
```

- **Replay.** Open the stream again into `/tmp/goforge-verify/proof/deploy-sse-replay.txt`. The replay file contains the log lines from the first file, not only `event: connected`.
- **Rows.** `verify.sh psql "select count(*) from deployment_logs where deployment_id = '$DEPLOYMENT_ID'"` prints a count greater than `0`. Write the count to `/tmp/goforge-verify/proof/deploy-verdict.txt`.
- **Build context.** `verify.sh psql "select build_context from projects where name = 'verify-hello'"` prints `app`. Since #28 the pipeline builds from that directory and rejects a context or Dockerfile that is missing or escapes the clone. This repository has no `app/` directory, so the deployment is expected to fail with `build context not found: app`. Record that as `deploy-build-context=pass` only when the failure names the context. For a build that succeeds, create a second project with `build_context=.`.
- **Proof.** The deployment detail `GET /projects/$PROJECT_ID/deployments/$DEPLOYMENT_ID` returns `200`. Record whether the body contains `No logs available yet` or a log line. Do not save the HTML.

- **Stage, reconnect, and rollback browser drive.** Use a throwaway host-mode app and Postgres when the live stack holds port 8080; host mode needs the Docker socket mounted or accessible so deploy and rollback work. Create a disposable public repository with two known-good commits and a deliberately slow valid Dockerfile. Deploy each revision, then drive Rollback from the newest row with Playwright. Capture the named restored target and working link, navigate away and return, and compare the original and restored statuses and log text. During a third deploy capture at least two real stages in the project banner and detail, force the SSE connection to reconnect by navigating away and back, and assert each stored log message appears exactly once. Create a separate project with a missing Dockerfile or build context to capture terminal failure feedback. For the live action swap, capture before/after screenshots, the persisted terminal status, SSE unnamed action-bar frame, and unchanged navigation counter. Save `revision.txt` as the exact PR head. No browser evidence means this is not a completed live drive.

## Gotchas

- t2166u slice 2 browser drive evidence is external proof; do not commit it. The feature expectations remain the source of truth.
- Pipeline log lines are stored in `deployment_logs`. A reconnect to the log stream sends one snapshot message with the stored lines before live log messages. Since htmx 4 these are unnamed messages (no `event:` line) carrying out-of-band swaps: the snapshot is `<div id="deployment-log-lines" hx-swap-oob="innerHTML">`, each line `<div hx-swap-oob="beforeend:#deployment-log-lines">`, and the status `<span id="deployment-live-status" hx-swap-oob="innerHTML">`. Do not treat `event: connected` alone as replay.
- The manual repository field is named `repository_url_manual` until page script renames it. HTTP posts must send `repository_url`.
- The log stream is behind `RequireAuth`. A missing session is `401` or a redirect to `/login`, not an empty log.
- A deployment id under a project it does not belong to, or another account's project, returns `404` on the detail page and the log stream.
- Status events update the badge on the deployment page.
