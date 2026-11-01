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
	// NotifyKindEmail sends a plain-text email over SMTP.
	NotifyKindEmail = "email"
)

// SMTP TLS modes stored in notify_settings.smtp_tls.
const (
	// SMTPTLSStartTLS connects in plain text and requires STARTTLS before
	// AUTH or MAIL (submission, usually port 587).
	SMTPTLSStartTLS = "starttls"
	// SMTPTLSImplicit opens TLS first (SMTPS, usually port 465).
	SMTPTLSImplicit = "implicit"
	// SMTPTLSNone never uses TLS. GoForge allows it only without a
	// username, or on a loopback host (a local relay).
	SMTPTLSNone = "none"
)

// SMTPSettings is the email notifier. Host, Username, Password, From, and
// To are sealed by the repository; To is a comma-separated address list.
type SMTPSettings struct {
	Host     string
	Port     int
	TLS      string
	Username string
	Password string
	From     string
	To       string
}

// NotifySettings is the one notify_settings row an admin saves in Settings →
// Profile → Notifications. The repository seals URL and Token with
// ENCRYPTION_KEY (and the SMTP secrets); in this struct they are plaintext.
type NotifySettings struct {
	Enabled   bool
	Kind      string
	URL       string
	Token     string
	SMTP      SMTPSettings
	UpdatedBy sql.NullString
	CreatedAt time.Time
	UpdatedAt time.Time
}
