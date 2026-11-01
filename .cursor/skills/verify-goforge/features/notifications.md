# Notifications and container health watch

An admin sets one outbound notifier on Settings → Profile → Notifications: an ntfy topic (plain text), a webhook (JSON POST), or email over SMTP (migration 028), with an optional bearer token (or SMTP username and password) and a Send test button. With it enabled, GoForge POSTs once when a queued deploy fails, a service run ends failed (with `last_error`), a backup run fails, or a tracked container that should be running goes unhealthy or exits. The payload holds the event, the resource, a redacted error, a link, and the `audit_id`, never a secret. The URL and token are sealed in `notify_settings` (migration 025).

## Sub-features

- `notify-settings-admin-edit`: an admin saves the form (`PUT /settings/notifications`), gets `200`, the re-rendered `#notify-settings` card, and the toast `Notification settings saved`; the `notify_settings` row exists with `enabled = true`.
- `notify-settings-nonadmin-403`: a non-admin sees no Notifications card, and `PUT /settings/notifications` and `POST /settings/notifications/test` answer `403`.
- `notify-settings-secret-masked`: neither the URL path nor the token is in any response (only `scheme://host`), the responses are `Cache-Control: no-store`, `url_sealed` and `token_sealed` hold no plaintext, and the `notify.settings.update` audit row carries only `{"changed": [field names]}`.
- `notify-settings-blank-keeps`: a save with blank URL and token keeps both (`No changes to save`, no audit row).
- `notify-token-stays-with-host`: with a token saved, a Send test or save to a URL on another scheme://host sends no `Authorization` header and a save drops the token (`changed` is `url,token`). Unit: `TestSettingsTokenStaysWithItsHost`.
- `notify-settings-validation`: a bad kind, a non-http(s) URL, a URL with `user:pw@`, or a `#fragment` is `400` naming the field, and nothing is stored.
- `notify-test-button`: Send test POSTs exactly one `event = "test"` payload to the form's URL (or the stored one), returns `200` `Test notification sent to <host>`, and saves nothing. A receiver answering non-2xx gives `502` `Test failed: <host> answered <code>` with no URL path.
- `notify-deploy-failed`: a failed queued deploy POSTs one `deploy.failed` with `resource.type = deployment`, `link` to `/projects/<id>/deployments/<id>`, and the run's `audit_id`. A canceled deploy POSTs nothing. Unit: `TestWorker_FailedDeployNotifiesOnce`, `TestWorker_SucceededOrCanceledDeployDoesNotNotify`.
- `notify-service-failed`: a failed service image update POSTs one `service.failed` whose `error` is the instance's `last_error`. Unit: `TestUpdateInstanceImage_FailedNotifiesWithLastError`.
- `notify-backup-failed`: a failed backup run POSTs one `backup.failed` with the run id and redacted summary. Unit: `TestRunner_FailedRunNotifiesOnce`, `TestManager_ScheduledFailedRunNotifies`.
- `notify-redaction`: `postgres://u:pw@`, `password=`, `api_key=`, `Bearer …`, and known secrets never reach the receiver (body or headers). Unit: `TestPayloadRedactsSecrets`, `TestRedactor`.
- `notify-email-save-sealed`: a save with `kind=email` and `smtp_host`, `smtp_port`, `smtp_tls`, `smtp_username`, `smtp_password`, `smtp_from`, `smtp_to` answers `200`; `kind = 'email'` in the row, `smtp_*_sealed` columns hold no plaintext, the card shows `value="email" selected` and "Remove the stored SMTP password" but never the password, and the audit row names only `smtp_*` fields. Unit: `TestNotifySettings_EmailSaveAndTestHideThePassword`, `TestNotifySettingsRepository_SealedRoundTrip`.
- `notify-email-test`: Send test with kind email delivers exactly one message (subject = alert title, `X-GoForge-Event: test`) to the SMTP server and returns `Test email sent via <host>:<port> to <n> recipient(s)`. A blank password uses the stored one; a new host drops it. Unit (fake SMTP in `notifytest.NewSMTPServer`, STARTTLS, implicit TLS, and none): `TestEmailSendsOneMessagePerTLSMode`, `TestEmailSettingsSaveTestAndKindSwitch`.
- `notify-email-auth-redacted`: a wrong password is `502` `Test failed: SMTP <host>:<port> rejected the username or password`; the fake server echoes the password in its 535 reply and neither the response nor the app log carries it. Unit: `TestEmailAuthFailureNeverLeaksThePassword`.
- `notify-email-validation`: bad TLS mode, host with scheme/port/user, port outside 1–65535, bad From, bad or >10 To, `none` with a username on a non-loopback host, and enabled without host/From/To are `400` naming the field. Unit: `TestEmailSettingsValidation`. Untrusted certificate and missing STARTTLS: `TestEmailUntrustedCertificateAndNoSTARTTLS`.
- `notify-kind-switch-keeps`: switching webhook → email → webhook keeps the URL and token, and the webhook Send test still works.
- `health-watch-once`: a tracked container that flips to unhealthy sends exactly one `container.unhealthy` over many ticks; flapping inside 30 minutes sends no more. Unit (fake Docker): `TestHealthWatchUnhealthyAlertsExactlyOnce`, `TestHealthWatchDedupeAndCooldown`, `TestHealthWatchPostsOnceToReceiver`.
- `health-watch-skips`: containers younger than 2 minutes, rows not `running`, and containers of a service that is not `running` are not inspected for alerts; with notifications off no Docker call is made. Unit: `TestHealthWatchSkips`, `TestHealthWatchDisabledSkipsDocker`.

## How to get to it (user POV)

- As the admin, open Settings → Profile. The Notifications card is below Backups.
- Pick ntfy or Webhook, enter the URL (and a token if the receiver needs one), press Send test, turn Enabled on, press Save notification settings.
- For email, pick Email (SMTP) and fill the Email (SMTP) fieldset (host, port, TLS, username, password, From, To) instead of the URL.
- Trigger a failure: a deploy of a project whose Dockerfile does not build, or stop a deployed container's process from inside so it exits (wait a minute for the health watch).

## Driving it with verify.sh

There is no `verify.sh drive-notifications` yet. Drive it with curl in host mode against a local receiver; never point it at a real ntfy topic.

1. Start host mode as in [backups](./backups.md) (throwaway `postgres:17-alpine`, `GOFORGE_PORT=18080`), then `drive-register` and `drive-login`.
2. Start a receiver on loopback that records each request, for example a 20-line Python `http.server` that appends method, path, headers, and body to `$EVIDENCE/notify-receiver.log` and answers `200` (or `403` for `/deny`).
3. With the admin cookie jar and CSRF token, `PUT /settings/notifications` with `enabled=true&kind=webhook&url=http://127.0.0.1:<port>/hook-<random>&token=<random>`, then `POST /settings/notifications/test`, then `POST /settings/notifications/test` with `url=http://127.0.0.1:<port>/deny`.
4. Check the receiver log (one test POST with `Authorization: Bearer`), the responses (no path, no token), `verify.sh psql -c "SELECT enabled, kind, url_sealed <> '' FROM notify_settings"`, and the `notify.settings.update` audit row details.

For email, run a loopback fake SMTP server (never a real relay): `notifytest.NewSMTPServer` from a throwaway `main` under an untracked directory, or any local sink that records the DATA. Use `smtp_tls=none` with host `127.0.0.1` (the only way `none` accepts a username) and `PUT` `kind=email&smtp_host=127.0.0.1&smtp_port=<port>&smtp_tls=none&smtp_username=<u>&smtp_password=<random>&smtp_from=...&smtp_to=...`, then Send test, then Send test with a wrong `smtp_password`. Check `SELECT kind, smtp_tls, smtp_password_sealed <> '' FROM notify_settings`, that `grep -c <password> host-app.log` is `0`, and that a `PUT` back to `kind=webhook` keeps the webhook URL. STARTTLS and implicit TLS against a fake with a self-signed cert are covered by the unit tests (the app trusts only system roots).

Deploy, service, backup, and health alerts need Docker; in host mode they are covered by the unit tests named above, which POST to an `httptest` receiver.

## Gotchas

- The health watch reads `containers.status`. A container stopped outside GoForge (`docker stop`) is still `running` in the table, so it alerts once as `container.exited`. `goforge backup restore-service` stops a service container for a while and can do the same if a tick lands then.
- A new container is skipped for 2 minutes, so a drive must wait past that before expecting a health alert.
- Delivery is one try with a 10 s limit. A slow or down receiver loses the alert; the app log says `notification not delivered` with the event and resource only.
- With `ENCRYPTION_KEY` changed, the saved URL does not decrypt: notifications stay off and the form asks for the URL again.
