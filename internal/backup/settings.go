package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Where the effective backup config came from.
const (
	// SourceSettings is the backup_settings row an admin saved.
	SourceSettings = "settings"
	// SourceEnvironment is BACKUP_* (no row yet).
	SourceEnvironment = "environment"
)

// probeTimeout bounds Test connection. It must finish well inside the 15 s
// write timeout of the Settings routes.
const probeTimeout = 10 * time.Second

// SettingsStore is the backup_settings row.
type SettingsStore interface {
	// Get returns repositories.ErrNotFound when no row exists.
	Get(ctx context.Context) (*models.BackupSettings, error)
	Upsert(ctx context.Context, s *models.BackupSettings) error
	InsertIfAbsent(ctx context.Context, s *models.BackupSettings) (bool, error)
}

// ConfigFromSettings converts a stored row to a BackupConfig.
func ConfigFromSettings(s *models.BackupSettings) config.BackupConfig {
	return config.BackupConfig{
		Endpoint: s.Endpoint, Region: s.Region, Bucket: s.Bucket, Prefix: s.Prefix,
		AccessKeyID: s.AccessKeyID, SecretAccessKey: s.SecretAccessKey, ForcePathStyle: s.ForcePathStyle,
		Schedule: s.Schedule, RetentionDaily: s.RetentionDaily, Timeout: s.Timeout,
		Disabled: !s.Enabled,
	}
}

// SettingsFromConfig converts a BackupConfig to a row.
func SettingsFromConfig(c config.BackupConfig) *models.BackupSettings {
	return &models.BackupSettings{
		Enabled:  !c.Disabled && strings.TrimSpace(c.Bucket) != "",
		Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, Prefix: c.Prefix,
		AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, ForcePathStyle: c.ForcePathStyle,
		Schedule: c.Schedule, RetentionDaily: c.RetentionDaily, Timeout: c.Timeout,
	}
}

// Effective returns the backup config serve, the status card, and
// `goforge backup run` use: the Settings row when one exists, else the
// BACKUP_* environment (precedence A). The source is SourceSettings or
// SourceEnvironment.
func Effective(ctx context.Context, store SettingsStore, env config.BackupConfig) (config.BackupConfig, string, error) {
	if store == nil {
		return env, SourceEnvironment, nil
	}
	row, err := store.Get(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return env, SourceEnvironment, nil
	case err != nil:
		return config.BackupConfig{}, SourceSettings, err
	}
	return ConfigFromSettings(row), SourceSettings, nil
}

// AutoImport copies the BACKUP_* environment into the Settings row on first
// start: only when no row exists and BACKUP_S3_BUCKET is set. It reports
// whether it wrote the row.
func AutoImport(ctx context.Context, store SettingsStore, env config.BackupConfig) (bool, error) {
	if store == nil || !env.Enabled() {
		return false, nil
	}
	return store.InsertIfAbsent(ctx, SettingsFromConfig(env))
}

// settingsFields lists the Settings form fields in display order, with how
// to compare two configs on each. Audit rows name the changed fields only.
var settingsFields = []struct {
	name string
	same func(a, b config.BackupConfig) bool
}{
	{"enabled", func(a, b config.BackupConfig) bool { return a.Enabled() == b.Enabled() }},
	{"endpoint", func(a, b config.BackupConfig) bool { return a.Endpoint == b.Endpoint }},
	{"region", func(a, b config.BackupConfig) bool { return a.Region == b.Region }},
	{"bucket", func(a, b config.BackupConfig) bool { return a.Bucket == b.Bucket }},
	{"prefix", func(a, b config.BackupConfig) bool { return a.Prefix == b.Prefix }},
	{"access_key_id", func(a, b config.BackupConfig) bool { return a.AccessKeyID == b.AccessKeyID }},
	{"secret_access_key", func(a, b config.BackupConfig) bool { return a.SecretAccessKey == b.SecretAccessKey }},
	{"force_path_style", func(a, b config.BackupConfig) bool { return a.ForcePathStyle == b.ForcePathStyle }},
	{"schedule", func(a, b config.BackupConfig) bool { return a.Schedule == b.Schedule }},
	{"retention_daily", func(a, b config.BackupConfig) bool { return a.RetentionDaily == b.RetentionDaily }},
	{"timeout", func(a, b config.BackupConfig) bool { return a.Timeout == b.Timeout }},
}

// ChangedFields names the fields that differ between a and b. It never
// returns values.
func ChangedFields(a, b config.BackupConfig) []string {
	var out []string
	for _, f := range settingsFields {
		if !f.same(a, b) {
			out = append(out, f.name)
		}
	}
	return out
}

// SettingsInput is the Settings form as submitted. Blank AccessKeyID and
// SecretAccessKey keep the stored values; ClearCredentials removes both.
type SettingsInput struct {
	Enabled          bool
	Endpoint         string
	Region           string
	Bucket           string
	Prefix           string
	AccessKeyID      string
	SecretAccessKey  string
	ClearCredentials bool
	ForcePathStyle   bool
	// ScheduleOff turns the nightly run off; otherwise ScheduleTime (HH:MM)
	// and ScheduleZone (IANA) make the schedule.
	ScheduleOff    bool
	ScheduleTime   string
	ScheduleZone   string
	RetentionDaily string
	Timeout        string
}

// Apply merges the form onto cur and returns the new config. It parses the
// number and duration fields (errors are *config.BackupFieldError) but does
// not run ValidateSettings.
func (in SettingsInput) Apply(cur config.BackupConfig) (config.BackupConfig, error) {
	next := cur
	next.Disabled = !in.Enabled
	next.Endpoint = strings.TrimSpace(in.Endpoint)
	next.Region = strings.TrimSpace(in.Region)
	next.Bucket = strings.TrimSpace(in.Bucket)
	next.Prefix = config.NormalizeBackupPrefix(in.Prefix)
	next.ForcePathStyle = in.ForcePathStyle
	if in.ClearCredentials {
		next.AccessKeyID, next.SecretAccessKey = "", ""
	}
	if v := strings.TrimSpace(in.AccessKeyID); v != "" {
		next.AccessKeyID = v
	}
	if v := strings.TrimSpace(in.SecretAccessKey); v != "" {
		next.SecretAccessKey = v
	}
	if in.ScheduleOff {
		next.Schedule = config.BackupScheduleOff
	} else {
		next.Schedule = strings.TrimSpace(in.ScheduleTime) + " " + strings.TrimSpace(in.ScheduleZone)
	}
	n, err := strconv.Atoi(strings.TrimSpace(in.RetentionDaily))
	if err != nil {
		return cur, &config.BackupFieldError{Field: "retention_daily", Message: "must be a whole number of days"}
	}
	next.RetentionDaily = n
	d, err := time.ParseDuration(strings.TrimSpace(in.Timeout))
	if err != nil {
		return cur, &config.BackupFieldError{Field: "timeout", Message: "must be a Go duration such as 2h or 90m"}
	}
	next.Timeout = d.Truncate(time.Second)
	return next, nil
}

// Applier swaps the running scheduler to a new config.
type Applier interface {
	Apply(cfg config.BackupConfig, source string)
}

// Prober lists one key under the prefix for Test connection.
type Prober func(ctx context.Context, cfg config.BackupConfig) (found bool, err error)

// ProbeS3 is the real Prober.
func ProbeS3(ctx context.Context, cfg config.BackupConfig) (bool, error) {
	store, err := NewS3Store(cfg)
	if err != nil {
		return false, err
	}
	return store.Probe(ctx, cfg.Prefix)
}

// SettingsService backs the admin Backups form on Settings → Profile.
type SettingsService struct {
	Store SettingsStore
	// Env is BACKUP_* from the environment, used until a row exists.
	Env config.BackupConfig
	// Live applies a saved config without a restart. May be nil in tests.
	Live Applier
	// Redactor scrubs app secrets and the env keys from probe errors; the
	// keys being tested are added per call.
	Redactor func(extra ...config.BackupConfig) *Redactor
	Probe    Prober
}

// SettingsView is what the form shows. It has no field for the secret, and
// only the last four characters of a long access key ID.
type SettingsView struct {
	Source         string
	Enabled        bool
	Endpoint       string
	Region         string
	Bucket         string
	Prefix         string
	AccessKeySet   bool
	AccessKeyHint  string
	SecretSet      bool
	ForcePathStyle bool
	ScheduleOff    bool
	ScheduleTime   string
	ScheduleZone   string
	RetentionDaily int
	Timeout        string
	UpdatedAt      *time.Time
}

// accessKeyHint is the last four characters of a long access key ID, or ""
// when the ID is too short to show any of it.
func accessKeyHint(id string) string {
	if len(id) < 12 {
		return ""
	}
	return id[len(id)-4:]
}

// NewSettingsView builds the form view for cfg.
func NewSettingsView(cfg config.BackupConfig, source string) *SettingsView {
	v := &SettingsView{
		Source: source, Enabled: cfg.Enabled(), Endpoint: cfg.Endpoint, Region: cfg.Region,
		Bucket: cfg.Bucket, Prefix: cfg.Prefix,
		AccessKeySet: cfg.AccessKeyID != "", AccessKeyHint: accessKeyHint(cfg.AccessKeyID),
		SecretSet: cfg.SecretAccessKey != "", ForcePathStyle: cfg.ForcePathStyle,
		RetentionDaily: cfg.RetentionDaily, Timeout: formatTimeout(cfg.Timeout),
	}
	if v.Region == "" {
		v.Region = config.DefaultBackupRegion
	}
	if v.RetentionDaily == 0 {
		v.RetentionDaily = config.DefaultBackupRetention
	}
	sched := cfg.Schedule
	if strings.TrimSpace(sched) == "" {
		sched = config.DefaultBackupSchedule
	}
	if strings.EqualFold(strings.TrimSpace(sched), config.BackupScheduleOff) {
		v.ScheduleOff = true
		def := strings.Fields(config.DefaultBackupSchedule)
		v.ScheduleTime, v.ScheduleZone = def[0], def[1]
	} else if f := strings.Fields(sched); len(f) == 2 {
		v.ScheduleTime, v.ScheduleZone = f[0], f[1]
	}
	return v
}

// formatTimeout prints 2h0m0s as 2h and 1h30m0s as 1h30m.
func formatTimeout(d time.Duration) string {
	if d <= 0 {
		d = config.DefaultBackupTimeout
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// current is the effective config the form edits.
func (s *SettingsService) current(ctx context.Context) (config.BackupConfig, string, error) {
	return Effective(ctx, s.Store, s.Env)
}

// View returns the form view.
func (s *SettingsService) View(ctx context.Context) (*SettingsView, error) {
	cfg, source, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	v := NewSettingsView(cfg, source)
	if source == SourceSettings && s.Store != nil {
		if row, rerr := s.Store.Get(ctx); rerr == nil {
			t := row.UpdatedAt
			v.UpdatedAt = &t
		}
	}
	return v, nil
}

// Save validates the form against the current settings, stores the row, and
// applies it live. It returns the changed field names (never values).
// Validation errors are *config.BackupFieldError and store nothing.
func (s *SettingsService) Save(ctx context.Context, in SettingsInput, userID string) ([]string, error) {
	cur, source, err := s.current(ctx)
	if err != nil {
		return nil, err
	}
	next, err := in.Apply(cur)
	if err != nil {
		return nil, err
	}
	if err := next.ValidateSettings(); err != nil {
		return nil, err
	}
	changed := ChangedFields(cur, next)
	if len(changed) == 0 && source == SourceSettings {
		return nil, nil
	}
	row := SettingsFromConfig(next)
	row.Enabled = in.Enabled
	if userID != "" {
		row.UpdatedBy = sql.NullString{String: userID, Valid: true}
	}
	if err := s.Store.Upsert(ctx, row); err != nil {
		return nil, err
	}
	if s.Live != nil {
		s.Live.Apply(ConfigFromSettings(row), SourceSettings)
	}
	return changed, nil
}

// ProbeError wraps a Test connection failure. Its message is redacted.
type ProbeError struct{ Message string }

func (e *ProbeError) Error() string { return e.Message }

// Test checks the form's destination (merged with the stored keys, so a
// blank secret tests the stored one) by listing at most one key under the
// prefix. It stores nothing. Validation errors are *config.BackupFieldError;
// a failed probe is *ProbeError with a redacted message.
func (s *SettingsService) Test(ctx context.Context, in SettingsInput) (string, error) {
	cur, _, err := s.current(ctx)
	if err != nil {
		return "", err
	}
	in.Enabled = true // the destination fields are required to test
	next, err := in.Apply(cur)
	if err != nil {
		return "", err
	}
	if verr := next.ValidateSettings(); verr != nil {
		return "", verr
	}
	probe := s.Probe
	if probe == nil {
		probe = ProbeS3
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	found, err := probe(ctx, next)
	if err != nil {
		red := NewRedactor(next.AccessKeyID, next.SecretAccessKey)
		if s.Redactor != nil {
			red = s.Redactor(next)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", &ProbeError{Message: fmt.Sprintf("no answer from %s within %s", next.Endpoint, probeTimeout)}
		}
		return "", &ProbeError{Message: red.Redact(err.Error())}
	}
	where := next.Bucket + "/" + next.Prefix
	if found {
		return "Connected. " + where + " is reachable and already holds objects.", nil
	}
	return "Connected. " + where + " is reachable and empty.", nil
}
