package notify

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// FieldError is a form validation error for one field.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// SettingsInput is the Notifications form as submitted. A blank URL or
// Token keeps the stored value; ClearToken removes the token. The SMTP
// fields apply only when Kind is email (and URL and Token only when it is
// not), so switching kinds keeps the other kind's settings. SMTPHost,
// SMTPUsername, SMTPFrom, and SMTPTo replace the stored values (the form
// shows them); a blank SMTPPassword keeps the stored one and
// ClearSMTPPassword removes it.
type SettingsInput struct {
	Enabled    bool
	Kind       string
	URL        string
	Token      string
	ClearToken bool

	SMTPHost          string
	SMTPPort          string
	SMTPTLS           string
	SMTPUsername      string
	SMTPPassword      string
	ClearSMTPPassword bool
	SMTPFrom          string
	SMTPTo            string
}

// maxSMTPRecipients caps the To list: this is an alert, not a mailing list.
const maxSMTPRecipients = 10

// SettingsView is what the form shows. It never holds the URL path or the
// token: only whether each is stored and the URL's scheme and host.
type SettingsView struct {
	Enabled   bool
	Kind      string
	URLSet    bool
	URLHost   string
	TokenSet  bool
	Saved     bool
	UpdatedAt *time.Time
	// Configured means the saved row has what its kind needs to send (a
	// URL, or an SMTP host, From, and To).
	Configured bool
	// SMTP fields for the email kind. The password is never in the view.
	SMTPHost        string
	SMTPPort        int
	SMTPTLS         string
	SMTPUsername    string
	SMTPFrom        string
	SMTPTo          string
	SMTPPasswordSet bool
	// Unreadable means the saved URL or token do not decrypt with the
	// current ENCRYPTION_KEY. Notifications are off until both are entered
	// again.
	Unreadable bool
}

// settingsState is the row the form edits.
type settingsState struct {
	row        models.NotifySettings
	saved      bool
	unreadable bool
}

// SettingsService backs the admin Notifications form.
type SettingsService struct {
	Store    Store
	BaseURL  string
	Redactor *Redactor
	// Client is used by Test; nil uses a 10 s client.
	Client *http.Client
	// SMTPTLS is the base TLS config for an email Test; nil uses the
	// system roots.
	SMTPTLS *tls.Config
}

func (s *SettingsService) current(ctx context.Context) (settingsState, error) {
	row, err := s.Store.Get(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return settingsState{row: models.NotifySettings{Kind: models.NotifyKindNtfy}}, nil
	case errors.Is(err, repositories.ErrNotifySettingsUnreadable) && row != nil:
		row.URL, row.Token = "", ""
		row.SMTP = models.SMTPSettings{Port: row.SMTP.Port, TLS: row.SMTP.TLS}
		return settingsState{row: *row, saved: true, unreadable: true}, nil
	case err != nil:
		return settingsState{}, err
	}
	return settingsState{row: *row, saved: true}, nil
}

// View returns the form view.
func (s *SettingsService) View(ctx context.Context) (*SettingsView, error) {
	st, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	v := &SettingsView{
		Enabled: st.row.Enabled && !st.unreadable, Kind: st.row.Kind,
		URLSet: st.row.URL != "", TokenSet: st.row.Token != "",
		Saved: st.saved, Unreadable: st.unreadable,
		Configured: configured(st.row),
		SMTPHost:   st.row.SMTP.Host, SMTPPort: st.row.SMTP.Port, SMTPTLS: st.row.SMTP.TLS,
		SMTPUsername: st.row.SMTP.Username, SMTPFrom: st.row.SMTP.From, SMTPTo: st.row.SMTP.To,
		SMTPPasswordSet: st.row.SMTP.Password != "",
	}
	if v.SMTPTLS == "" {
		v.SMTPTLS = models.SMTPTLSStartTLS
	}
	if v.Kind == "" {
		v.Kind = models.NotifyKindNtfy
	}
	if v.URLSet {
		v.URLHost = hostOf(st.row.URL)
	}
	if st.saved {
		t := st.row.UpdatedAt
		v.UpdatedAt = &t
	}
	return v, nil
}

// validateURL accepts an absolute http or https URL with a host.
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return &FieldError{Field: "url", Message: "must be an http:// or https:// URL"}
	}
	if u.User != nil {
		return &FieldError{Field: "url", Message: "must not contain a user or password; use the token field"}
	}
	if u.Fragment != "" {
		return &FieldError{Field: "url", Message: "must not contain a #fragment"}
	}
	return nil
}

// apply merges the form onto the current row and validates the result.
func (st settingsState) apply(in SettingsInput) (models.NotifySettings, error) {
	next := st.row
	next.Enabled = in.Enabled
	next.Kind = strings.TrimSpace(in.Kind)
	switch next.Kind {
	case models.NotifyKindNtfy, models.NotifyKindWebhook:
	case models.NotifyKindEmail:
		return st.applySMTP(next, in)
	default:
		return next, &FieldError{Field: "kind", Message: "must be ntfy, webhook, or email"}
	}
	if v := strings.TrimSpace(in.URL); v != "" {
		if err := validateURL(v); err != nil {
			return next, err
		}
		// A saved token goes only to the host it was entered for. Moving
		// the URL to another scheme://host drops it unless one is typed.
		if next.Token != "" && hostOf(v) != hostOf(st.row.URL) {
			next.Token = ""
		}
		next.URL = v
	}
	if in.ClearToken {
		next.Token = ""
	}
	if v := strings.TrimSpace(in.Token); v != "" {
		next.Token = v
	}
	if st.unreadable && strings.TrimSpace(in.URL) == "" && next.Enabled {
		return next, &FieldError{Field: "url", Message: "the saved URL does not decrypt with the current ENCRYPTION_KEY; enter it again"}
	}
	if next.Enabled && next.URL == "" {
		return next, &FieldError{Field: "url", Message: "is required to turn notifications on"}
	}
	return next, nil
}

// applySMTP merges the SMTP fields onto next (kind email) and validates.
func (st settingsState) applySMTP(next models.NotifySettings, in SettingsInput) (models.NotifySettings, error) {
	c := next.SMTP
	c.TLS = strings.TrimSpace(in.SMTPTLS)
	if c.TLS == "" {
		c.TLS = models.SMTPTLSStartTLS
	}
	if c.TLS != models.SMTPTLSStartTLS && c.TLS != models.SMTPTLSImplicit && c.TLS != models.SMTPTLSNone {
		return next, &FieldError{Field: "smtp_tls", Message: "must be starttls, implicit, or none"}
	}
	host := strings.TrimSpace(in.SMTPHost)
	if strings.ContainsAny(host, " \t\r\n/@:") && net.ParseIP(host) == nil {
		return next, &FieldError{Field: "smtp_host", Message: "must be a host name or IP address, without a scheme, port, or user"}
	}
	// A saved password goes only to the host it was entered for.
	if c.Password != "" && host != st.row.SMTP.Host {
		c.Password = ""
	}
	c.Host = host
	c.Port = defaultSMTPPort(c.TLS)
	if v := strings.TrimSpace(in.SMTPPort); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			return next, &FieldError{Field: "smtp_port", Message: "must be a number from 1 to 65535"}
		}
		c.Port = n
	}
	c.Username = strings.TrimSpace(in.SMTPUsername)
	if in.ClearSMTPPassword {
		c.Password = ""
	}
	if in.SMTPPassword != "" {
		c.Password = in.SMTPPassword
	}
	c.From = strings.TrimSpace(in.SMTPFrom)
	c.To = strings.TrimSpace(in.SMTPTo)
	if err := validateSMTP(c); err != nil {
		return next, err
	}
	next.SMTP = c
	if next.Enabled {
		return next, st.requireSMTP(c)
	}
	return next, nil
}

// validateSMTP checks From, To, and the TLS mode against the username.
func validateSMTP(c models.SMTPSettings) error {
	if c.From != "" {
		if _, err := mail.ParseAddress(c.From); err != nil {
			return &FieldError{Field: "smtp_from", Message: "must be one email address"}
		}
	}
	if c.To != "" {
		list, err := mail.ParseAddressList(c.To)
		if err != nil {
			return &FieldError{Field: "smtp_to", Message: "must be email addresses separated by commas"}
		}
		if len(list) > maxSMTPRecipients {
			return &FieldError{Field: "smtp_to", Message: "may list at most 10 addresses"}
		}
	}
	if c.TLS == models.SMTPTLSNone && c.Username != "" && c.Host != "" && !isLoopbackHost(c.Host) {
		return &FieldError{Field: "smtp_tls", Message: "none would send the password in clear text; use STARTTLS or TLS (none with a username is only for localhost)"}
	}
	return nil
}

// requireSMTP is the check for an enabled email notifier (and Test).
func (st settingsState) requireSMTP(c models.SMTPSettings) error {
	for _, req := range []struct{ field, v string }{{"smtp_host", c.Host}, {"smtp_from", c.From}, {"smtp_to", c.To}} {
		if req.v == "" {
			msg := "is required to turn email notifications on"
			if st.unreadable {
				msg = "does not decrypt with the current ENCRYPTION_KEY; enter it again"
			}
			return &FieldError{Field: req.field, Message: msg}
		}
	}
	return nil
}

// changedFields names the fields that differ. It never returns values.
func changedFields(a, b models.NotifySettings) []string {
	var out []string
	if a.Enabled != b.Enabled {
		out = append(out, "enabled")
	}
	if a.Kind != b.Kind {
		out = append(out, "kind")
	}
	if a.URL != b.URL {
		out = append(out, "url")
	}
	if a.Token != b.Token {
		out = append(out, "token")
	}
	for _, f := range []struct {
		name string
		x, y string
	}{
		{"smtp_host", a.SMTP.Host, b.SMTP.Host},
		{"smtp_port", strconv.Itoa(a.SMTP.Port), strconv.Itoa(b.SMTP.Port)},
		{"smtp_tls", a.SMTP.TLS, b.SMTP.TLS},
		{"smtp_username", a.SMTP.Username, b.SMTP.Username},
		{"smtp_password", a.SMTP.Password, b.SMTP.Password},
		{"smtp_from", a.SMTP.From, b.SMTP.From},
		{"smtp_to", a.SMTP.To, b.SMTP.To},
	} {
		if f.x != f.y {
			out = append(out, f.name)
		}
	}
	return out
}

// Save validates and stores the form. It returns the changed field names
// (never values). Validation errors are *FieldError and store nothing.
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
	if len(changed) == 0 {
		changed = []string{}
	}
	return changed, nil
}

// Test sends one test event to the form's target (blank fields use the
// stored values). Nothing is saved. Delivery errors are *SendError.
func (s *SettingsService) Test(ctx context.Context, in SettingsInput) (string, error) {
	st, err := s.current(ctx)
	if err != nil {
		return "", err
	}
	in.Enabled = true
	next, err := st.apply(in)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	ev := Event{
		Kind:     EventTest,
		Resource: Resource{Type: "notifier", ID: next.Kind},
		Error:    "This is a test from GoForge. Failures will look like this.",
		Path:     "/settings/profile",
		Time:     time.Now(),
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	tgt := Target{Kind: next.Kind, URL: next.URL, Token: next.Token, SMTP: next.SMTP}
	if err := deliver(ctx, client, s.SMTPTLS, tgt, buildPayload(ev, s.BaseURL, s.Redactor)); err != nil {
		return "", err
	}
	if next.Kind == models.NotifyKindEmail {
		n := 0
		if list, err := mail.ParseAddressList(next.SMTP.To); err == nil {
			n = len(list)
		}
		return fmt.Sprintf("Test email sent via %s to %d recipient(s)", smtpAddr(next.SMTP), n), nil
	}
	return "Test notification sent to " + hostOf(next.URL), nil
}
