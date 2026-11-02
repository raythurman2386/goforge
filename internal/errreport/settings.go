package errreport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Store reads and writes the error_reporting_settings row
// (repositories.ErrorReportingSettingsRepository).
type Store interface {
	Get(ctx context.Context) (*models.ErrorReportingSettings, error)
	Upsert(ctx context.Context, s *models.ErrorReportingSettings) error
}

// Live is the running reporter a save hot-applies to (*Reporter).
type Live interface {
	Apply(c Config) error
	SendTest(ctx context.Context, c Config, timeout time.Duration) (TestResult, error)
}

// Source says where the DSN in effect comes from.
const (
	SourceNone     = ""
	SourceSettings = "settings"
	SourceEnv      = "environment"
)

// testTimeout bounds Send test.
const testTimeout = 5 * time.Second

// FieldError is a form validation error for one field.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// SendError is a Send test that did not reach the Sentry host.
type SendError struct{ Message string }

func (e *SendError) Error() string { return e.Message }

// SettingsInput is the form as submitted. A blank DSN keeps the stored
// one; ClearDSN removes it (error reporting off).
type SettingsInput struct {
	DSN         string
	ClearDSN    bool
	Environment string
}

// SettingsView is what the card shows. It never holds the DSN key: only
// whether one is set and its scheme://host/project.
type SettingsView struct {
	DSNSet  bool
	DSNHint string
	// Source is SourceSettings, SourceEnv (SENTRY_DSN, no row saved), or
	// SourceNone (off).
	Source string
	// Environment is the saved value ('' = automatic); EffectiveEnvironment
	// is what events carry.
	Environment          string
	EffectiveEnvironment string
	Saved                bool
	UpdatedAt            *time.Time
	// Unreadable means the saved DSN does not decrypt with the current
	// ENCRYPTION_KEY; error reporting is off until it is entered again.
	Unreadable bool
}

// SettingsService backs the admin Error reporting card.
type SettingsService struct {
	Store Store
	Live  Live
	// EnvDSN is SENTRY_DSN, used only while no row is saved.
	EnvDSN string
	// DefaultEnvironment is the automatic environment (development in dev
	// mode, otherwise production).
	DefaultEnvironment string
}

type settingsState struct {
	row        models.ErrorReportingSettings
	saved      bool
	unreadable bool
}

func (s *SettingsService) current(ctx context.Context) (settingsState, error) {
	row, err := s.Store.Get(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return settingsState{}, nil
	case errors.Is(err, repositories.ErrErrorReportingSettingsUnreadable) && row != nil:
		row.DSN = ""
		return settingsState{row: *row, saved: true, unreadable: true}, nil
	case err != nil:
		return settingsState{}, err
	}
	return settingsState{row: *row, saved: true}, nil
}

// effective is the config in force for st and where its DSN comes from.
func (s *SettingsService) effective(st settingsState) (c Config, source string) {
	c = Config{DSN: st.row.DSN, Environment: st.row.Environment}
	switch {
	case st.saved && c.DSN != "":
		return c, SourceSettings
	case !st.saved && strings.TrimSpace(s.EnvDSN) != "":
		c.DSN = strings.TrimSpace(s.EnvDSN)
		return c, SourceEnv
	}
	return Config{Environment: c.Environment}, SourceNone
}

// Effective returns the config to bind at startup. A saved row always wins
// over SENTRY_DSN, so saving a blank DSN turns reporting off even when the
// env var is set. An unreadable row is off.
func (s *SettingsService) Effective(ctx context.Context) (Config, string, error) {
	st, err := s.current(ctx)
	if err != nil {
		return Config{}, SourceNone, err
	}
	c, src := s.effective(st)
	return c, src, nil
}

// View returns the card view.
func (s *SettingsService) View(ctx context.Context) (*SettingsView, error) {
	st, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	c, src := s.effective(st)
	v := &SettingsView{
		DSNSet: c.DSN != "", Source: src,
		Environment: st.row.Environment, EffectiveEnvironment: s.envOf(c),
		Saved: st.saved, Unreadable: st.unreadable,
	}
	if v.DSNSet {
		v.DSNHint = DSNHint(c.DSN)
	}
	if st.saved {
		t := st.row.UpdatedAt
		v.UpdatedAt = &t
	}
	return v, nil
}

func (s *SettingsService) envOf(c Config) string {
	if e := strings.TrimSpace(c.Environment); e != "" {
		return e
	}
	if s.DefaultEnvironment != "" {
		return s.DefaultEnvironment
	}
	return "production"
}

// environmentRe is Sentry's environment rule: no spaces, slashes, or
// newlines, at most 64 characters.
var environmentRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateDSN checks raw parses as a Sentry DSN and uses https, or http on
// a loopback host (a local relay or test receiver).
func ValidateDSN(raw string) error {
	d, err := sentry.NewDsn(raw)
	if err != nil {
		return &FieldError{Field: "dsn", Message: "is not a valid Sentry DSN (https://<key>@<host>/<project>)"}
	}
	if d.GetSecretKey() != "" {
		return &FieldError{Field: "dsn", Message: "must not contain a secret key; use the public DSN from Sentry → Client Keys"}
	}
	switch d.GetScheme() {
	case "https":
	case "http":
		if !isLoopbackHost(d.GetHost()) {
			return &FieldError{Field: "dsn", Message: "must use https:// (http:// is only allowed for localhost)"}
		}
	default:
		return &FieldError{Field: "dsn", Message: "must use https://"}
	}
	return nil
}

func isLoopbackHost(h string) bool {
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// apply merges the form onto the current row and validates the result.
func (st settingsState) apply(in SettingsInput) (models.ErrorReportingSettings, error) {
	next := st.row
	if in.ClearDSN {
		next.DSN = ""
	}
	if v := strings.TrimSpace(in.DSN); v != "" {
		if err := ValidateDSN(v); err != nil {
			return next, err
		}
		next.DSN = v
	}
	env := strings.TrimSpace(in.Environment)
	if env != "" && !environmentRe.MatchString(env) {
		return next, &FieldError{Field: "environment", Message: "must be up to 64 letters, digits, dots, dashes, or underscores"}
	}
	next.Environment = env
	return next, nil
}

// changedFields names the fields that differ. It never returns values.
func changedFields(a, b models.ErrorReportingSettings) []string {
	var out []string
	if a.DSN != b.DSN {
		out = append(out, "dsn")
	}
	if a.Environment != b.Environment {
		out = append(out, "environment")
	}
	return out
}

// Save validates and stores the form, then hot-applies it to Live (no
// restart). It returns the changed field names (never values).
// Validation errors are *FieldError and store nothing.
func (s *SettingsService) Save(ctx context.Context, in SettingsInput, userID string) ([]string, error) {
	st, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	next, err := st.apply(in)
	if err != nil {
		return nil, err
	}
	changed := changedFields(st.row, next)
	if len(changed) == 0 && st.saved && !st.unreadable {
		return nil, nil
	}
	if userID != "" {
		next.UpdatedBy = sql.NullString{String: userID, Valid: true}
	}
	if err := s.Store.Upsert(ctx, &next); err != nil {
		return nil, err
	}
	if s.Live != nil {
		c, _ := s.effective(settingsState{row: next, saved: true})
		if err := s.Live.Apply(c); err != nil {
			return nil, fmt.Errorf("saved, but error reporting did not start: %w", err)
		}
	}
	if len(changed) == 0 {
		changed = []string{}
	}
	return changed, nil
}

// Test sends one test event to the form's DSN (blank uses the one in
// effect, including SENTRY_DSN). Nothing is saved and the live client is
// untouched. A blank result is a *FieldError; a failed delivery is a
// *SendError.
func (s *SettingsService) Test(ctx context.Context, in SettingsInput) (string, error) {
	st, err := s.current(ctx)
	if err != nil {
		return "", err
	}
	next, err := st.apply(in)
	if err != nil {
		return "", err
	}
	c, _ := s.effective(settingsState{row: next, saved: st.saved || in.ClearDSN || strings.TrimSpace(in.DSN) != ""})
	if c.DSN == "" {
		msg := "is empty, so error reporting is off; enter a DSN to send a test"
		if st.unreadable {
			msg = "does not decrypt with the current ENCRYPTION_KEY; enter it again"
		}
		return "", &FieldError{Field: "dsn", Message: msg}
	}
	if s.Live == nil {
		return "", errors.New("error reporting is not wired")
	}
	res, err := s.Live.SendTest(ctx, c, testTimeout)
	hint := DSNHint(c.DSN)
	if err != nil {
		if errors.Is(err, ErrNotDelivered) {
			msg := "test event not delivered to " + hint
			if res.Status != 0 {
				msg += fmt.Sprintf(" (HTTP %d)", res.Status)
			} else {
				msg += " (unreachable)"
			}
			return "", &SendError{Message: msg}
		}
		return "", err
	}
	return fmt.Sprintf("Test event sent to %s (event %s)", hint, res.EventID), nil
}
