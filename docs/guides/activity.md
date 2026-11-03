# Activity and audit log

**Settings → Activity** (`/settings/activity`) lists the audit log, newest
first: every change made in the web UI, through `/api/v1`, or by an MCP agent.

## What a row shows

| Column | Meaning |
|--------|---------|
| Time | When the row was written |
| Source | `MCP` (user agent starts with `goforge-mcp`), `API` (an `api_v1.` action or `details.via` = `api_v1`), `Webhook` (`details.via` = `webhook`), `CLI` (`details.via` = `cli`, a host command such as `goforge user reset-2fa`), or `UI` |
| Action | The action, for example `service.delete` or `api_v1.restart_service`. It links to the change and its outcome rows |
| Token | The API token name that made the change (API and MCP rows) |
| Resource | A link to the project, service, deployment, environment's project, container, API token, or Git source |
| Approval | The `approval_ref` of an approved API or MCP change |
| Error | Why the change failed |

Rows whose action ends in `.failed` are tinted red.

## Filters

- **Source**: Agents (MCP or API), MCP, API, UI, Webhook, or CLI.
- **Token name**: exact token name, for example `ci-bot`.
- **Action**: part of the action, ignoring case (`restart`, `delete`).
- `?audit_id=<id>` shows one change and every row whose `details.audit_id`
  points at it. The Action link sets it.
- Admins can tick **All users** (`all=1`). Everyone else sees only their own
  rows.

The page shows 50 rows; **Older** and **Newer** page through the rest.

The dashboard's **Recent agent actions** card shows your latest five MCP and
API rows and links to the page filtered to agents.

## What is recorded

- Web UI changes, with `details.via` = `ui`: project create, update,
  settings, delete, link and unlink a service; deployment trigger, rollback,
  and cancel; environment create and update; variable set (key and
  `is_secret`, never the value), delete, and sync; service deploy, delete,
  restart, config edit (field names, never values), and image update;
  container stop and restart.
- Every `/api/v1` and `/mcp` write, recorded before it acts, as before.
- Login, logout, registration, API token create, rotate, and revoke, backup
  settings, and `goforge backup restore`, as before.
- Settings changes about your account, with `details.via` = `ui`: password
  change (`user.password.change`) and **Sign out other sessions**
  (`user.sessions.revoke`), both with `sessions_revoked`; Profile save
  (`user.profile.update`) with `changed` and `old_email_domain` and
  `new_email_domain`, never the full address; Git source add and remove
  (`git_source.create`, `git_source.delete`) with the name, type,
  `has_access_token`, and `has_api_url`, never the token, SSH key, or URL.
- Two-factor authentication: `user.2fa.enable` (with `recovery_codes`),
  `user.2fa.disable` (with `second_factor`, the kind of code used),
  `login.2fa_failed` for each wrong code at `/auth/2fa` (with `attempt` and
  `challenge_ended`), and `user.2fa.reset` from `goforge user reset-2fa`
  (`details.via` = `cli`, source **CLI**). A `login` row after the second
  step has `second_factor` = `totp` or `recovery_code`. The key and the codes
  are never recorded.
- Webhook deploys: each deployment a signed GitHub, Gitea, or GitLab push
  starts writes `deployment.trigger` with `details.via` = `webhook`,
  `provider`, `branch`, `commit`, `repository`, `project_id`, and
  `environment_id`. The row's user is the project owner, and its IP and user
  agent are the webhook sender's. A push that matches no auto-deploy
  environment, or has a bad signature, writes nothing.

## Retention and tamper detection

Rows are kept forever unless you set a retention. Admins set it in
Settings → Profile → **Audit log**, or with `GOFORGE_AUDIT_RETENTION_DAYS`
until someone saves the card (a saved value wins). `0` keeps every row.
Any other value must be 30 to 3650 days. The page above the table says
which rule is in effect. About five minutes after startup, and then once
a day, GoForge hard-deletes old rows when doing so does not punch a hole
in the hash chain: a prefix of the chain, a suffix of rows sealed after
every row still inside the window, and unsealed rows older than the
cutoff. A backdated row sealed between two newer rows stays. The job
writes one `audit.retention.purge` row (`deleted`, `cutoff`,
`through_seq`, `through_hash`) in the same transaction that moves the
chain boundary, when it deleted something.
Saving the card writes `audit.settings.update` with `old_days` and
`new_days`.

The 30-day floor is there so an API `approval_ref` cannot be replayed by
waiting for the row to disappear. Deletes younger than 30 days are refused
even by the purge job.

`UPDATE`, `DELETE`, and `TRUNCATE` on `audit_logs` and `audit_log_seals`
are rejected unless the transaction has set `goforge.audit_purge` to `on`.
Any database session can set that flag. The table owner can also
`DISABLE TRIGGER`. The triggers stop accidents and buggy writes. They are
not a security boundary. Detection is the hash chain. When the stored
boundary is past seq 0, `goforge audit verify` requires an
`audit.retention.purge` row whose `through_seq` and `through_hash` match
it and whose `purge_mac` is an HMAC of those fields under the chain key.
A sealed purge row is not enough: the sealer seals every row, including
one inserted by someone who can write the database. A missing or wrong
MAC exits 1 with `boundary without purge`. The MAC is checked as soon as
the purge transaction commits, before that row is sealed.
Deleting a user does not blank `user_id` (the foreign key was dropped in
migration 031) so a sealed row stays byte-for-byte the same.

Every row older than about five seconds is sealed into an HMAC-SHA256
chain. The key is derived from `ENCRYPTION_KEY` and is not stored.
`goforge audit verify` exits 0 when the chain is intact and prints
`audit chain verified head_seq=N head_hash=...`. It exits 1 and names the
first bad seq when a row was changed, removed, or left unsealed, when the
retention boundary does not match, or when that boundary has no
chain-key `purge_mac` (`boundary without purge`). `goforge serve` runs the same check
about 30 seconds after startup and then daily, and logs that line. Each
backup `manifest.json` includes `audit_chain_head` with the same seq and
hash. Admins see the last result on this page.

What this does not do:

- It does not stop someone who can read `ENCRYPTION_KEY`. The Docker
  socket and root on the host can. They can drop the triggers, rewrite
  the rows, recompute the chain, and replace the log line and the backup.
- A row deleted in the few seconds before it is sealed leaves no seal.
- Rows that were already in the database at upgrade are only proven
  unchanged since the first seal after the upgrade, not since they were
  written.
- There is no separate database role that can only insert. The app still
  connects as the table owner.

Keep a copy of the nightly backup off the host if you need a record that
a host administrator cannot rewrite. A backup restored onto an install
with a different `ENCRYPTION_KEY` will fail `goforge audit verify`, which
is expected.

## Outcomes of queued work

A queued deploy or service operation that runs after the request
returns writes an outcome row when it finishes:

| Action | Written when |
|--------|--------------|
| `deployment.run.completed` | The queued deployment finished |
| `deployment.run.failed` | It failed; `details.error` says why (500 characters at most) |
| `deployment.run.canceled` | It was canceled |
| `service.run.completed` / `service.run.failed` | A service deploy, config edit, or image update finished or failed |

The outcome row's `details.audit_id` is the id of the row that started the
change (the UI row, the `api_v1.*` row, or the webhook `deployment.trigger`
row), and it copies that row's user,
IP, user agent, `via`, `approval_ref`, `token_name`, and project. So a
failed deploy started by an MCP agent shows up as an MCP row with the
agent's token and approval. The queue keeps the id in
`deployment_queue.audit_id` (migration `024`), so a deploy that waits in
the queue or runs after a restart still links back.

If a webhook deployment cannot be queued, `deployment.trigger.failed` is
written with `details.audit_id` set to its trigger row.

A UI change that is refused before it runs (for example a service that is
already busy) writes `<action>.failed` with `details.audit_id` set to the
row it started.

`api_v1.<action>.completed` and `.failed` rows from the API still exist and
now carry `token_id` and `token_name` too.
