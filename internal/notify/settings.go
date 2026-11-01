package notify

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
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
// Token keeps the stored value; ClearToken removes the token.
type SettingsInput struct {
	Enabled    bool
	Kind       string
	URL        string
	Token      string
	ClearToken bool
}

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
}

func (s *SettingsService) current(ctx context.Context) (settingsState, error) {
	row, err := s.Store.Get(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return settingsState{row: models.NotifySettings{Kind: models.NotifyKindNtfy}}, nil
	case errors.Is(err, repositories.ErrNotifySettingsUnreadable) && row != nil:
		row.URL, row.Token = "", ""
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
	if next.Kind != models.NotifyKindNtfy && next.Kind != models.NotifyKindWebhook {
		return next, &FieldError{Field: "kind", Message: "must be ntfy or webhook"}
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
	if err := deliver(ctx, client, Target{Kind: next.Kind, URL: next.URL, Token: next.Token}, buildPayload(ev, s.BaseURL, s.Redactor)); err != nil {
		return "", err
	}
	return "Test notification sent to " + hostOf(next.URL), nil
}
