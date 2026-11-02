package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
)

func renderNotify(t *testing.T, v *notify.SettingsView) string {
	t.Helper()
	var b strings.Builder
	if err := NotifyCard(v, "tok").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestNotifyCard_FormAndTestButton(t *testing.T) {
	out := renderNotify(t, &notify.SettingsView{Enabled: true, Kind: models.NotifyKindWebhook, URLSet: true, URLHost: "https://hooks.example.test", TokenSet: true, Saved: true})
	for _, want := range []string{
		`id="notify-settings"`, `hx-put="/settings/notifications"`, `hx-post="/settings/notifications/test"`,
		`name="enabled"`, `name="kind"`, `name="url"`, `name="token"`, `name="clear_token"`, `type="password"`,
		`value="webhook" selected`, "https://hooks.example.test/••••••••", "Notifications are on",
		`name="csrf_token" value="tok"`, "Send test", "Save notification settings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("card missing %q", want)
		}
	}
}

func TestNotifyCard_EmailFields(t *testing.T) {
	out := renderNotify(t, &notify.SettingsView{Enabled: true, Kind: models.NotifyKindEmail, Configured: true, Saved: true,
		SMTPHost: "smtp.example.test", SMTPPort: 465, SMTPTLS: models.SMTPTLSImplicit, SMTPUsername: "alerts@example.test",
		SMTPFrom: "goforge@example.test", SMTPTo: "ops@example.test", SMTPPasswordSet: true})
	for _, want := range []string{
		`value="email" selected`, `value="ntfy"`, `value="webhook"`, `name="smtp_host"`, `value="smtp.example.test"`,
		`name="smtp_port"`, `value="465"`, `value="implicit" selected`, `value="starttls"`, `value="none"`,
		`name="smtp_username"`, `name="smtp_password"`, `name="clear_smtp_password"`, `name="smtp_from"`, `name="smtp_to"`,
		"Set. Sent only to the saved SMTP host.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("email card missing %q", want)
		}
	}
}

func TestNotifyCard_UnsetAndUnreadable(t *testing.T) {
	out := renderNotify(t, &notify.SettingsView{Kind: models.NotifyKindNtfy})
	if !strings.Contains(out, "Add an ntfy topic, a webhook URL, or an SMTP server") || strings.Contains(out, `name="clear_token"`) {
		t.Errorf("unset card: %s", out)
	}
	out = renderNotify(t, &notify.SettingsView{Kind: models.NotifyKindNtfy, Unreadable: true, Saved: true})
	if !strings.Contains(out, "does not decrypt with the current ENCRYPTION_KEY") {
		t.Errorf("unreadable card: %s", out)
	}
}

func TestProfile_NotifyCardOnlyWhenGiven(t *testing.T) {
	user := &models.User{Email: "a@x", Role: models.RoleAdmin}
	var b strings.Builder
	if err := Profile(user, "tok", nil, nil, &notify.SettingsView{Kind: "ntfy"}, nil, nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `id="notify-settings"`) {
		t.Error("admin profile has no Notifications card")
	}
	b.Reset()
	if err := Profile(user, "tok", nil, nil, nil, nil, nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), `id="notify-settings"`) {
		t.Error("Notifications card shown without a form")
	}
}
