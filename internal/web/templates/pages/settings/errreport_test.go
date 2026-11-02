package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/errreport"
	"github.com/raythurman2386/goforge/internal/models"
)

func renderErrReport(t *testing.T, v *errreport.SettingsView) string {
	t.Helper()
	var b strings.Builder
	if err := ErrorReportingCard(v, "tok").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestErrorReportingCard_FormButtonsAndMaskedDSN(t *testing.T) {
	out := renderErrReport(t, &errreport.SettingsView{DSNSet: true, DSNHint: "https://o1.ingest.sentry.io/42", Source: errreport.SourceSettings, EffectiveEnvironment: "production", Saved: true})
	for _, want := range []string{
		`id="error-reporting-settings"`, `hx-put="/settings/error-reporting"`, `hx-post="/settings/error-reporting/test"`,
		`name="dsn"`, `type="password"`, `name="clear_dsn"`, `name="environment"`, `name="csrf_token" value="tok"`,
		"Error reporting (Sentry)", "Error reporting is on (from settings)", "https://o1.ingest.sentry.io/42", "Send test event", "Save error reporting",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("card missing %q", want)
		}
	}
}

func TestErrorReportingCard_OffEnvAndUnreadable(t *testing.T) {
	out := renderErrReport(t, &errreport.SettingsView{EffectiveEnvironment: "production"})
	if !strings.Contains(out, "Error reporting is off") || strings.Contains(out, `name="clear_dsn"`) {
		t.Errorf("off card: %s", out)
	}
	out = renderErrReport(t, &errreport.SettingsView{DSNSet: true, DSNHint: "https://h/1", Source: errreport.SourceEnv, EffectiveEnvironment: "production"})
	if !strings.Contains(out, "from the environment: SENTRY_DSN") || strings.Contains(out, `name="clear_dsn"`) {
		t.Errorf("env card: %s", out)
	}
	out = renderErrReport(t, &errreport.SettingsView{Unreadable: true, Saved: true, EffectiveEnvironment: "production"})
	if !strings.Contains(out, "does not decrypt with the current ENCRYPTION_KEY") {
		t.Errorf("unreadable card: %s", out)
	}
}

func TestErrorReportingCard_OnProfileOnlyWhenGiven(t *testing.T) {
	user := &models.User{Email: "a@x", Role: models.RoleAdmin}
	var b strings.Builder
	if err := Profile(user, "tok", nil, nil, nil, &errreport.SettingsView{EffectiveEnvironment: "production"}, nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `id="error-reporting-settings"`) {
		t.Fatal("profile missing the card")
	}
	b.Reset()
	if err := Profile(user, "tok", nil, nil, nil, nil, nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "error-reporting-settings") {
		t.Fatal("card shown without a view")
	}
}
