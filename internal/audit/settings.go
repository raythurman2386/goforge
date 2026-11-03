package audit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Where the effective retention came from.
const (
	// SourceSettings is a row an admin saved.
	SourceSettings = "settings"
	// SourceEnvironment is GOFORGE_AUDIT_RETENTION_DAYS, used while no
	// admin has saved the card.
	SourceEnvironment = "environment"
)

// FieldError is one retention form field that did not validate.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// Store reads and writes audit_settings.
type Store interface {
	Get(ctx context.Context) (*models.AuditSettings, error)
	SaveRetention(ctx context.Context, days int, updatedBy string) error
}

// SettingsService is the admin retention card and the value the purge reads.
type SettingsService struct {
	Store Store
	// EnvDays and EnvSet are GOFORGE_AUDIT_RETENTION_DAYS. EnvSet is false
	// when the variable is unset or blank.
	EnvDays int
	EnvSet  bool
}

// SettingsView is what the Profile card shows. It never includes a secret.
type SettingsView struct {
	Days     int
	Source   string
	ReadOnly bool
	// EnvIgnored is true when a saved row wins over a set environment variable.
	EnvIgnored bool
	UpdatedAt  *time.Time
}

// ValidDays reports whether n is 0 (keep forever) or inside the retention range.
func ValidDays(n int) bool {
	return n == 0 || (n >= MinRetentionDays && n <= MaxRetentionDays)
}

// ParseRetentionDays parses a form or environment value.
func ParseRetentionDays(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, &FieldError{Field: "retention_days", Message: "is required"}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || !ValidDays(n) {
		return 0, &FieldError{Field: "retention_days", Message: "must be 0 (keep forever) or between 30 and 3650"}
	}
	return n, nil
}

// RetentionNote is the one line on Settings → Activity.
func RetentionNote(days int) string {
	if days <= 0 {
		return "Rows are kept until deleted by hand."
	}
	return fmt.Sprintf("Rows older than %d days are deleted daily.", days)
}

// CheckEnv fails serve when GOFORGE_AUDIT_RETENTION_DAYS is invalid and no
// admin has saved a row. A saved row wins, and the bad environment is ignored.
func CheckEnv(ctx context.Context, store Store, envErr error) error {
	if envErr == nil {
		return nil
	}
	if store == nil {
		return envErr
	}
	row, err := store.Get(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return envErr
	case err != nil:
		return fmt.Errorf("failed to read audit settings: %w", err)
	case row.RetentionSaved:
		return nil
	default:
		return envErr
	}
}

// Effective returns the retention in force and its source. Source is empty
// when nothing is configured and rows are kept forever.
func (s *SettingsService) Effective(ctx context.Context) (days int, source string, err error) {
	if s == nil {
		return 0, "", nil
	}
	var row *models.AuditSettings
	if s.Store != nil {
		got, getErr := s.Store.Get(ctx)
		switch {
		case errors.Is(getErr, repositories.ErrNotFound):
		case getErr != nil:
			return 0, "", getErr
		default:
			row = got
		}
	}
	if row != nil && row.RetentionSaved {
		return row.RetentionDays, SourceSettings, nil
	}
	if s.EnvSet {
		return s.EnvDays, SourceEnvironment, nil
	}
	return 0, "", nil
}

// View returns the card.
func (s *SettingsService) View(ctx context.Context) (*SettingsView, error) {
	days, source, err := s.Effective(ctx)
	if err != nil {
		return nil, err
	}
	v := &SettingsView{
		Days:       days,
		Source:     source,
		ReadOnly:   source == SourceEnvironment,
		EnvIgnored: s != nil && s.EnvSet && source == SourceSettings,
	}
	if s == nil || s.Store == nil || source != SourceSettings {
		return v, nil
	}
	row, getErr := s.Store.Get(ctx)
	if errors.Is(getErr, repositories.ErrNotFound) {
		return v, nil
	}
	if getErr != nil {
		return nil, getErr
	}
	t := row.UpdatedAt
	v.UpdatedAt = &t
	return v, nil
}

// Save validates raw and stores it. changed is false when the saved value
// is already in effect. The environment source is read-only.
func (s *SettingsService) Save(ctx context.Context, raw, userID string) (oldDays, newDays int, changed bool, err error) {
	oldDays, source, err := s.Effective(ctx)
	if err != nil {
		return 0, 0, false, err
	}
	if source == SourceEnvironment {
		return 0, 0, false, &FieldError{Field: "retention_days", Message: "is set by GOFORGE_AUDIT_RETENTION_DAYS"}
	}
	newDays, err = ParseRetentionDays(raw)
	if err != nil {
		return 0, 0, false, err
	}
	changed = source != SourceSettings || newDays != oldDays
	if !changed {
		return oldDays, newDays, false, nil
	}
	if s == nil || s.Store == nil {
		return 0, 0, false, errors.New("audit settings are not available")
	}
	if err := s.Store.SaveRetention(ctx, newDays, userID); err != nil {
		return 0, 0, false, err
	}
	return oldDays, newDays, true, nil
}
