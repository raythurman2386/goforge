# Notifications

GoForge can send an alert to one outbound notifier when something fails: an [ntfy](https://ntfy.sh) topic or a webhook that takes a JSON POST. An admin sets it up in **Settings → Profile → Notifications**. A save applies at once, with no restart.

## What sends an alert

| Event | When | Link |
|-------|------|------|
| `deploy.failed` | A queued deployment fails. A canceled deployment sends nothing. | The deployment page |
| `service.failed` | A service deploy, config edit, or image update ends with the service failed. The error is the service's `last_error`. | The service page |
| `backup.failed` | A backup run (nightly or `goforge backup run`) ends `failed`. A run that cannot start, for example because another run holds the lock, sends nothing. | Settings → Profile, Backups card |
| `container.unhealthy` | A tracked container's Docker health check reports `unhealthy`. | The container page |
| `container.exited` | A tracked container that should be running is `exited`, `dead`, or `restarting`. | The container page |

GoForge sends each alert once. A failed deploy or service is reported by the run that failed, not again on every page load.

## Set it up

1. Open **Settings → Profile** as an admin. The **Notifications** card is under **Backups**.
2. Pick **Type**:
    - **ntfy topic**: GoForge POSTs plain text to the topic URL, with `Title`, `Tags`, `Priority` (`high` for failures), and `Click` (the link) headers.
    - **Webhook**: GoForge POSTs the JSON payload below with `Content-Type: application/json`.
3. Enter the **URL**, for example `https://ntfy.sh/<your-topic>` or your self-hosted ntfy. It must be `http://` or `https://`, with no `user:password@` and no `#fragment`.
4. Optionally enter an **Access token**. GoForge sends it as `Authorization: Bearer <token>` (an ntfy access token, or your webhook's).
5. Press **Send test**. GoForge sends one test notification to the URL in the form (or the saved one if the field is blank) and shows `Test notification sent to <host>`, or why it failed (`<host> answered 403`, `could not reach <host>`, `timed out`). Nothing is saved.
6. Turn **Enabled** on and press **Save notification settings**.

The URL and the token are stored encrypted with `ENCRYPTION_KEY`, because an ntfy topic or a webhook path is itself a secret. After a save, the form shows only the scheme and host (`https://ntfy.sh/••••••••`). Leave the URL or token blank to keep the saved value. The saved token is only sent to the host it was entered for: a URL on another scheme or host (in a save or a test) goes without it unless you type the token again. Check **Remove the stored token** to drop the token.

Every save is written to the audit log as `notify.settings.update`, with the names of the changed fields only (`enabled`, `kind`, `url`, `token`), never their values.

If the saved URL no longer decrypts (for example after `ENCRYPTION_KEY` changed), notifications stay off and the card asks you to enter the URL again.

## Payload

The webhook body:

```json
{
  "source": "goforge",
  "event": "deploy.failed",
  "title": "GoForge: Deployment failed: shop / production",
  "resource": {"type": "deployment", "id": "6f1c…", "name": "shop / production"},
  "error": "build failed: …",
  "link": "https://forge.example.com/projects/2b9e…/deployments/6f1c…",
  "audit_id": "0b5c7c8e-…",
  "time": "2026-11-01T14:03:11Z"
}
```

- `resource.type` is `deployment`, `service_instance`, `backup_run`, `container`, or `notifier` (a test).
- `error` is redacted and at most 500 bytes. See below.
- `link` is `GOFORGE_BASE_URL` plus the page path. It is empty when there is no page.
- `audit_id` is the audit row that started the run (a UI action, an `/api/v1` call, or an MCP tool). Use it with the `audit_id` filter on [Settings → Activity](activity.md). It is empty for nightly backups and container health alerts.

The ntfy body is the error, then `<resource type> <resource id>`, then `audit_id <id>` when there is one.

## What is never sent

The payload never holds passwords, tokens, or connection strings. Before an error leaves GoForge it is redacted:

- the values GoForge knows are secret: `ENCRYPTION_KEY`, `SESSION_SECRET`, `CSRF_SECRET`, `WEBHOOK_SECRET`, the GitHub client secret, the database password, and the backup S3 keys
- the password in any `scheme://user:password@host` URL
- the value of any `key=value` or `key: value` pair whose key contains `password`, `secret`, `token`, `api_key`, `access_key`, `private_key`, or `credential`
- `Bearer …` and `Basic …` credentials

The notifier's own URL and token are never in the payload or in GoForge's log. Log lines name the event and resource only (`notification sent`, `notification not delivered`).

## Container health watch

While notifications are on, `goforge serve` inspects every tracked container once a minute. It watches a container when:

- its row in `containers` is `running` (a container you stopped from GoForge is `exited` and not watched)
- it was created more than 2 minutes ago (a deploy's own health check reports a bad new container as `deploy.failed`)
- for a service container, the service is `running` (a stopped or failed service is not watched)

A watched container that is `unhealthy`, `exited`, `dead`, or `restarting` sends one alert. It does not alert again while it stays bad. When it recovers and fails again, it alerts again, but at most once every 30 minutes per container, so a restart loop does not page every minute. A container Docker no longer knows is skipped.

While notifications are off, the watch does nothing and makes no Docker calls.

## Limits

- One notifier for the whole instance. Email and per-project channels are not supported yet.
- Delivery is best effort: one try with a 10 second limit. A failed delivery is logged and not retried.
- A container stopped outside GoForge (`docker stop`), or stopped by `goforge backup restore-service` while a tick runs, can send one `container.exited` alert.
