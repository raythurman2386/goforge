package settings

import (
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/web/templates/components/ui"
)

// notifyKindOptions are the notifier types in the form.
func notifyKindOptions() []ui.SelectOption {
	return []ui.SelectOption{
		{Value: models.NotifyKindNtfy, Label: "ntfy topic (plain text)"},
		{Value: models.NotifyKindWebhook, Label: "Webhook (JSON POST)"},
	}
}

// notifyUnreadableNote is shown when the saved URL or token do not decrypt
// with the current ENCRYPTION_KEY.
const notifyUnreadableNote = "Saved, but it does not decrypt with the current ENCRYPTION_KEY. Enter it again."

func notifyStatusLine(v *notify.SettingsView) string {
	switch {
	case v.Unreadable:
		return "Notifications are off: the saved URL does not decrypt with the current ENCRYPTION_KEY."
	case v.Enabled:
		return "Notifications are on. Failed deployments, failed services, failed backups, and unhealthy or exited containers send one alert each."
	case v.URLSet:
		return "Notifications are off. The URL is saved."
	default:
		return "Notifications are off. Add an ntfy topic or webhook URL to get alerts."
	}
}

func notifyURLState(v *notify.SettingsView) string {
	if v.Unreadable {
		return notifyUnreadableNote
	}
	if v.URLSet {
		return "Set (" + v.URLHost + "/…). Leave blank to keep it; type a new one to replace it."
	}
	return "Not set. The path is kept secret: it is stored encrypted and never shown again."
}

func notifyURLPlaceholder(v *notify.SettingsView) string {
	if v.URLSet {
		return v.URLHost + "/••••••••"
	}
	return "https://ntfy.sh/<your-topic>"
}

func notifyTokenState(v *notify.SettingsView) string {
	if v.Unreadable {
		return notifyUnreadableNote
	}
	if v.TokenSet {
		return "Set. Sent as Authorization: Bearer, only to the saved host. Leave blank to keep it."
	}
	return "Optional. Sent as Authorization: Bearer (an ntfy access token, or your webhook's)."
}

func notifyTokenPlaceholder(v *notify.SettingsView) string {
	if v.TokenSet {
		return "••••••••"
	}
	return ""
}
