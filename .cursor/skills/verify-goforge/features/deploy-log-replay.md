# Deploy with log replay

Deploy lets a signed-in user create a project from a public Git URL, start a build, and read the same log lines again after the stream reconnects.

## Sub-features

- `deploy-create` saves a project and its `production` environment.
- `deploy-trigger` starts a deployment from the `Deploy Now` button.
- `deploy-sse-replay` returns the earlier log lines on a second connection to the log stream.
- `deploy-db-rows` shows those lines in `deployment_logs`.

## How to get to it (user POV)

- In the sidebar, choose `Projects`, then `New Project`. The page heading is `Create Project`.
- Submit `Create Project`.
- On the project page, choose `Deploy Now`.
- Open the deployment. The log pane id is `deployment-logs`. While the deployment is not terminal, that pane connects to the log stream.

## Driving it with verify.sh

Preconditions:

- `verify.sh doctor` reports `drive_auth=yes` and `drive_docker=yes`.
- `drive-register` has already created the bootstrap session. Reuse `/tmp/goforge-verify/jar-a.txt`.
- The public repository `https://github.com/raythurman2386/goforge.git` is reachable from the `goforge` container.

- **Create the project.** Read `goforge_csrf` from `jar-a.txt` into a shell variable. Do not print it. Post the form.

```bash
curl -sS -D /tmp/goforge-verify/project.headers -o /dev/null \
  -b /tmp/goforge-verify/jar-a.txt -c /tmp/goforge-verify/jar-a.txt \
  -X POST http://127.0.0.1:8080/projects \
  --data-urlencode "csrf_token=$csrf" \
  --data-urlencode "name=verify-hello" \
  --data-urlencode "repository_url=https://github.com/raythurman2386/goforge.git" \
  --data-urlencode "default_branch=main" \
  --data-urlencode "dockerfile_path=Dockerfile" \
  --data-urlencode "build_context=."
```

The response is `303` and `Location` matches `/projects/` plus an id. `GET` that URL with the same jar returns `200` and the body contains `verify-hello`.

- **Start the deploy.** Read the production environment id with `verify.sh psql "select e.id from environments e join projects p on p.id = e.project_id where p.name = 'verify-hello'"`. Post `Deploy Now`.

```bash
curl -sS -D /tmp/goforge-verify/deploy.headers -o /dev/null \
  -b /tmp/goforge-verify/jar-a.txt -c /tmp/goforge-verify/jar-a.txt \
  -H "X-CSRF-Token: $csrf" \
  -X POST "http://127.0.0.1:8080/projects/$PROJECT_ID/deployments?env_id=$ENV_ID"
```

The response is `200` and the `HX-Redirect` header is `/projects/$PROJECT_ID/deployments`. Read the new deployment id with `verify.sh psql "select d.id from deployments d join environments e on e.id = d.environment_id join projects p on p.id = e.project_id where p.name = 'verify-hello' order by d.created_at desc limit 1"`.

- **First stream.** Capture about 10 seconds, then stop that curl.

```bash
curl -sS -N --max-time 10 \
  -b /tmp/goforge-verify/jar-a.txt \
  "http://127.0.0.1:8080/projects/$PROJECT_ID/deployments/$DEPLOYMENT_ID/logs" \
  > artifacts/verify-goforge-proof/deploy-sse-first.txt
```

- **Replay.** Open the stream again into `artifacts/verify-goforge-proof/deploy-sse-replay.txt`. The replay file contains the log lines from the first file, not only `event: connected`.
- **Rows.** `verify.sh psql "select count(*) from deployment_logs where deployment_id = '$DEPLOYMENT_ID'"` prints a count greater than `0`. Write the count to `artifacts/verify-goforge-proof/deploy-verdict.txt`.
- **Proof.** The deployment detail `GET /projects/$PROJECT_ID/deployments/$DEPLOYMENT_ID` returns `200`. Record whether the body contains `No logs available yet` or a log line. Do not save the HTML.

## Gotchas

- `Service.Log` in `internal/deploy/state.go` writes structured process logs and returns. It does not insert `deployment_logs`. `SSEHandler.DeploymentLogs` sends `event: connected` and then live hub events. It does not replay stored rows. On this tree `deploy-sse-replay` and `deploy-db-rows` fail. Record the count and that the replay file has no earlier log line. Do not treat `event: connected` as replay.
- The manual repository field is named `repository_url_manual` until page script renames it. HTTP posts must send `repository_url`.
- Build context is stored on the project and the pipeline clones into a work directory. `BuildImage` uses that clone root as `ContextDir`. A `build_context` other than `.` is not applied. This journey uses `.` so that gap is not the assertion.
- The log stream is behind `RequireAuth`. A missing session is `401` or a redirect to `/login`, not an empty log.
- Status broadcasts use the deployment topic. The log pane swaps on SSE event name `log`. A status payload does not append a log line and does not change the badge until reload.
