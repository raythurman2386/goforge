# Error reporting (Sentry)

An admin sets an optional Sentry DSN on Settings → Profile → Error reporting (Sentry). With one, GoForge sends panics in web, `/api/v1`, and in-process `/mcp` handlers, and every handled `5xx` page (`RenderError`), to Sentry. The DSN is sealed in `error_reporting_settings` (migration 029). Blank is off; a save hot-applies (`errreport.Reporter.Apply`: a new client, or `BindClient(nil)` for off). `SENTRY_DSN` is a fallback only while no row exists. Tracing is off.

## Sub-features

- `errreport-settings-admin-edit`: an admin `PUT /settings/error-reporting` with `dsn` and `environment` gets `200`, the `#error-reporting-settings` card, the toast `Error reporting settings saved`, and the status `Error reporting is on (from settings)`; the row has `dsn_sealed <> ''`.
- `errreport-settings-nonadmin-403`: a non-admin sees no card; `PUT /settings/error-reporting` and `POST /settings/error-reporting/test` answer `403` (`401` anonymous) and nothing is saved or sent. Unit: `TestErrorReportSettings_NonAdminForbidden`.
- `errreport-dsn-masked`: no response holds the DSN key (only `scheme://host/project`), responses are `Cache-Control: no-store`, `dsn_sealed` has no plaintext, and the `error_reporting.settings.update` audit row has only `{"changed": [...]}`. Unit: `TestErrorReportSettings_SaveTestRemoveWithoutRestart`, `TestErrorReport_DSNHintNeverHasKey`, `TestErrorReportingSettingsRepository_SealedRoundTrip`.
- `errreport-blank-keeps`: a save with a blank DSN keeps it (`No changes to save`, no audit row).
- `errreport-validation`: a DSN that does not parse, has a secret key, or is `http://` to a non-loopback host, and a bad environment, are `400` naming the field, never echoing the DSN; nothing is stored. Unit: `TestErrorReportSettings_BadDSNIs400AndNotEchoed`, `TestErrorReportSettings_Validation`.
- `errreport-test-button`: Send test event delivers exactly one envelope to `<dsn host>/api/<project>/envelope/` through a throwaway client and returns `Test event sent to <hint> (event <id>)`; a non-2xx ingest is `502`. Unit: `TestErrorReport_SendTestDeliversOneEnvelope`.
- `errreport-remove-off`: **Remove the stored DSN** turns it off at once (no restart); Send test then answers `400` "error reporting is off" and the ingest gets nothing. Unit: `TestErrorReport_BlankApplySendsNothingWithoutRestart`.
- `errreport-env-fallback`: `SENTRY_DSN` turns it on only with no row; a saved blank row wins, and an unreadable row is off. Unit: `TestErrorReportSettings_EnvFallbackOnlyWithoutRow`, `TestErrorReport_BlankDSNIgnoresSentryDSNEnv`, `TestErrorReportSettings_UnreadableIsOff`.
- `errreport-capture`: a handler panic is one event and still a `500`; a `RenderError` 500/502 is one event tagged `http.route` (chi pattern), method, status; a 404 or 400 is none; `http.ErrAbortHandler` is dropped; inner recovers in `apiv1` and `mcphttp` capture. Unit: `TestErrorReport_PanicCapturedRedactedAndStillA500`, `TestErrorReport_RenderError500CapturedWithRoutePattern`, `TestErrorReport_RenderError500CapturedOnRequestHub`, `TestErrorReport_AbortHandlerDropped`, `TestErrorReport_CapturePanicFromInnerRecover`.
- `errreport-scrub`: events carry no `Cookie`, `Authorization`, CSRF or API-key header, query string, body, client IP, or user details, and secrets in messages are redacted. Unit: `TestErrorReport_PanicCapturedRedactedAndStillA500`.

## How to get to it (user POV)

- As the admin, open Settings → Profile. The Error reporting (Sentry) card is below Notifications.
- Paste a DSN from Sentry → Project settings → Client Keys, press Send test event, then Save error reporting. Tick Remove the stored DSN and save to turn it off.

## Driving it in host mode

There is no `verify.sh drive-error-reporting` yet. Drive it with curl in host mode against a loopback fake ingest; never point it at a real Sentry project.

1. Start host mode as in [backups](./backups.md) (throwaway `postgres:17-alpine`, `GOFORGE_PORT=18080`), then `drive-register` and `drive-login`.
2. Start a receiver on loopback that counts `POST .../envelope/` and answers `200` (a 20-line Python `http.server`). The DSN is `http://<publickey>@127.0.0.1:<port>/1`.
3. With the admin jar and CSRF token: `PUT /settings/error-reporting` with `dsn=...`, then `POST /settings/error-reporting/test` (blank `dsn` uses the saved one): the receiver has 1 envelope. `verify.sh psql -c "SELECT dsn_sealed <> '', position('<publickey>' in dsn_sealed) FROM error_reporting_settings"` gives `t | 0`.
4. As a second (non-admin) user, both routes are `403`.
5. `PUT` with `clear_dsn=true`, then Send test: `400` "error reporting is off", the receiver count does not move. No restart between steps.

## Gotchas

- Send test uses its own client, so it proves the DSN works without changing what is live; Save is what turns reporting on.
- The SDK would read `SENTRY_DSN` itself for an empty `Dsn`; GoForge never builds a client with an empty DSN (`rg 'Dsn: *""'` stays empty) and binds the noop client for off.
- With `ENCRYPTION_KEY` changed, the saved DSN does not decrypt: reporting is off (even with `SENTRY_DSN` set) and the card asks for it again.
- Background goroutine panics (worker, health watch, backups) are not reported in v1.
