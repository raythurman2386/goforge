package settings

import (
	"database/sql"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/raythurman2386/goforge/internal/audit"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/errreport"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
)

func TestSettingsPages_OneSharedNavigationCurrent(t *testing.T) {
	local := &models.User{Email: "local@example.test"}
	adminViews := []*backup.Status{{Enabled: false}}
	pages := []struct {
		name string
		page templ.Component
		href string
	}{
		{"profile", Profile(local, "csrf"), "/settings/profile"},
		{"security", Security(local, "csrf", nil, nil), "/settings/security"},
		{"activity", Activity(nil, ActivityFilters{}, "csrf"), "/settings/activity"},
		{"settings", GitSources(nil, "csrf", adminViews[0], nil, &notify.SettingsView{}, &errreport.SettingsView{}, &audit.SettingsView{}), "/settings/git"},
	}
	for _, tc := range pages {
		t.Run(tc.name, func(t *testing.T) {
			got := renderMCP(t, tc.page)
			if n := strings.Count(got, `aria-label="Settings pages"`); n != 1 {
				t.Fatalf("shared nav landmarks = %d", n)
			}
			current := regexp.MustCompile(`<a href="([^"]+)" aria-current="page"`).FindAllStringSubmatch(got, -1)
			if len(current) != 1 || current[0][1] != tc.href {
				t.Fatalf("settings current links = %v, want %s", current, tc.href)
			}
			for _, href := range []string{"/settings/profile", "/settings/security", "/settings/activity", "/settings/git"} {
				if !strings.Contains(got, `href="`+href+`"`) {
					t.Errorf("missing settings nav link %s", href)
				}
			}
		})
	}
}

func TestSettingsPages_SectionPlacementAndHTMXContracts(t *testing.T) {
	local := &models.User{Email: "local@example.test"}
	profile := renderMCP(t, Profile(local, "csrf"))
	security := renderMCP(t, Security(local, "csrf", NewMCPCard("https://gf.example.test"), &TwoFactorCard{PasswordAccount: true}))
	settings := renderMCP(t, GitSources(nil, "csrf", &backup.Status{}, &backup.SettingsView{}, &notify.SettingsView{}, &errreport.SettingsView{}, &audit.SettingsView{}))
	activity := renderMCP(t, Activity(nil, ActivityFilters{}, "csrf"))
	for _, moved := range []string{"Change Password", "Two-factor authentication", "Sessions", `id="mcp-card"`, "API Tokens", "Backups", "Notifications", "Error reporting", "Audit log"} {
		if strings.Contains(profile, moved) {
			t.Errorf("Profile still contains %q", moved)
		}
	}
	for _, want := range []string{
		`hx-put="/settings/password"`, `hx-post="/settings/2fa/setup"`,
		`id="sessions-list" hx-get="/settings/sessions" hx-trigger="load, sessionRevoked from:body" hx-swap="innerHTML"`,
		`id="mcp-card"`, `id="tokens-list" hx-get="/settings/tokens" hx-trigger="load, tokenCreated from:body" hx-swap="innerHTML"`,
	} {
		if !strings.Contains(security, want) {
			t.Errorf("Security missing %q", want)
		}
	}
	for _, wrong := range []string{"Backups", "Notifications", "Error reporting", "Audit log"} {
		if strings.Contains(security, wrong) {
			t.Errorf("Security contains admin section %q", wrong)
		}
	}
	for _, want := range []string{
		"Git Sources", `id="backup-status"`, `id="backup-restore-points"`,
		`hx-get="/settings/backups/restore-points"`, `hx-put="/settings/backups" hx-target="#backup-status" hx-swap="outerHTML"`,
		`id="notify-settings"`, `hx-put="/settings/notifications" hx-target="#notify-settings" hx-swap="outerHTML"`,
		`id="error-reporting-settings"`, `hx-put="/settings/error-reporting" hx-target="#error-reporting-settings" hx-swap="outerHTML"`,
		`id="audit-retention-settings"`, `hx-put="/settings/audit-retention" hx-target="#audit-retention-settings" hx-swap="outerHTML"`,
	} {
		if !strings.Contains(settings, want) {
			t.Errorf("Settings missing %q", want)
		}
	}
	if !strings.Contains(activity, `action="/settings/activity"`) || strings.Contains(activity, `hx-put="/settings/audit-retention"`) {
		t.Error("Activity content changed or owns audit retention")
	}
}

func TestSecurity_GitHubAccountHasNoPasswordForm(t *testing.T) {
	gh := &models.User{Email: "gh@example.test", GitHubID: sql.NullInt64{Int64: 1, Valid: true}}
	got := html.UnescapeString(renderMCP(t, Security(gh, "csrf", nil, &TwoFactorCard{GitHubLinked: true})))
	if strings.Contains(got, "Change Password") || strings.Contains(got, `hx-put="/settings/password"`) {
		t.Error("GitHub account has local password form")
	}
	for _, want := range []string{"GitHub account's two-factor authentication", `id="sessions-list"`} {
		if !strings.Contains(got, want) {
			t.Errorf("GitHub Security missing %q", want)
		}
	}
}
