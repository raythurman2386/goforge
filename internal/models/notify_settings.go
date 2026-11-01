package models

import (
	"database/sql"
	"time"
)

// Notifier kinds stored in notify_settings.kind.
const (
	// NotifyKindNtfy posts a plain-text message to an ntfy topic URL.
	NotifyKindNtfy = "ntfy"
	// NotifyKindWebhook posts a JSON body to a generic webhook URL.
	NotifyKindWebhook = "webhook"
)

// NotifySettings is the one notify_settings row an admin saves in Settings →
// Profile → Notifications. The repository seals URL and Token with
// ENCRYPTION_KEY; in this struct they are plaintext.
type NotifySettings struct {
	Enabled   bool
	Kind      string
	URL       string
	Token     string
	UpdatedBy sql.NullString
	CreatedAt time.Time
	UpdatedAt time.Time
}
