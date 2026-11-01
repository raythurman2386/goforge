// Package notify sends one outbound alert per failure to the URL an admin
// saved in Settings → Profile → Notifications: an ntfy topic (plain text
// with Title, Tags, Priority, and Click headers) or a generic JSON webhook.
//
// Events fire when a queued deployment fails, a service deploy or change
// fails, a backup run fails, and when the health watch (internal/monitor)
// sees a tracked container go unhealthy or exit. A payload carries the
// event, the resource, a redacted error, a link into GoForge, and the
// audit id when there is one. It never carries the notifier URL, the
// token, or any other secret; Redactor scrubs the error text.
package notify
