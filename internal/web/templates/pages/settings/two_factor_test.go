package settings

import (
	"bytes"
	"context"
	"database/sql"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func renderProfileTF(t *testing.T, u *models.User, c *TwoFactorCard) string {
	t.Helper()
	var b bytes.Buffer
	if err := Profile(u, "tok", nil, nil, nil, nil, nil, c).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(b.String())
}

func TestProfileTwoFactorCard(t *testing.T) {
	local := &models.User{Email: "u@x", PasswordHash: sql.NullString{String: "h", Valid: true}}
	if got := renderProfileTF(t, local, nil); strings.Contains(got, "two-factor-card") {
		t.Error("nil card rendered")
	}
	off := renderProfileTF(t, local, &TwoFactorCard{PasswordAccount: true})
	if !strings.Contains(off, `hx-post="/settings/2fa/setup"`) || strings.Contains(off, "/settings/2fa/disable") {
		t.Error("off card should offer setup only")
	}
	on := renderProfileTF(t, local, &TwoFactorCard{PasswordAccount: true, Enabled: true, EnabledAt: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), RecoveryCodesLeft: 9})
	for _, want := range []string{`hx-post="/settings/2fa/disable"`, "On since Nov 2, 2026", "9 of 10 recovery codes left."} {
		if !strings.Contains(on, want) {
			t.Errorf("on card missing %q", want)
		}
	}
	gh := renderProfileTF(t, &models.User{Email: "g@x", GitHubID: sql.NullInt64{Int64: 1, Valid: true}}, &TwoFactorCard{GitHubLinked: true})
	if !strings.Contains(gh, "GitHub account's two-factor authentication") || strings.Contains(gh, "/settings/2fa/setup") {
		t.Error("GitHub-only card should explain GitHub 2FA and offer no setup")
	}
}

func TestRecoveryCodesModalShowsCodes(t *testing.T) {
	var b bytes.Buffer
	if err := RecoveryCodesModal([]string{"aaaaa-bbbbb", "ccccc-ddddd"}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "aaaaa-bbbbb\nccccc-ddddd") {
		t.Errorf("codes not rendered: %s", b.String())
	}
}
