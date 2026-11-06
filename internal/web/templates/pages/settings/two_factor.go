package settings

import (
	"strings"
	"time"
)

// TwoFactorCard is the Settings → Security two-factor card. A nil card hides
// it (2FA not wired). PasswordAccount is false for GitHub-only accounts,
// which see a note that GitHub's own 2FA applies instead.
type TwoFactorCard struct {
	PasswordAccount   bool
	GitHubLinked      bool
	Enabled           bool
	EnabledAt         time.Time
	RecoveryCodesLeft int
}

// TwoFactorSetup is the enrollment dialog: the QR code as a PNG data: URI
// and the same secret grouped for manual entry.
type TwoFactorSetup struct {
	QRDataURI    string
	ManualSecret string
}

func twoFactorEnabledLabel(c *TwoFactorCard) string {
	if c.EnabledAt.IsZero() {
		return "On"
	}
	return "On since " + c.EnabledAt.UTC().Format("Jan 2, 2006")
}

func recoveryCodesText(codes []string) string {
	return strings.Join(codes, "\n")
}
