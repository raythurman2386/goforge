package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	// Embed the IANA zone database so BACKUP_SCHEDULE zones resolve even on
	// an image without tzdata.
	_ "time/tzdata"
)

// Backup defaults.
const (
	DefaultBackupSchedule  = "02:00 America/Chicago"
	DefaultBackupPrefix    = "goforge/"
	DefaultBackupRegion    = "auto"
	DefaultBackupRetention = 7
	DefaultBackupTimeout   = 2 * time.Hour
	// BackupScheduleOff turns the in-app scheduler off. `goforge backup run`
	// still works.
	BackupScheduleOff = "off"
)

// BackupConfig configures GoForge-managed backups to S3-compatible storage
// (Cloudflare R2, MinIO, Backblaze B2). Backups are disabled when Bucket is
// empty or Disabled is set. The access key and secret are never logged or
// rendered.
//
// It comes from the BACKUP_* environment, or from the backup_settings row an
// admin saves in Settings → Profile. Once that row exists it wins over the
// environment (see internal/backup.Effective).
type BackupConfig struct {
	// Endpoint is the S3 API endpoint, for example
	// https://<account>.r2.cloudflarestorage.com or http://127.0.0.1:9000.
	// A missing scheme means https.
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	// ForcePathStyle uses bucket-in-path URLs (MinIO). R2 and B2 work either way.
	ForcePathStyle bool
	// Schedule is "HH:MM <IANA zone>" or "off".
	Schedule string
	// RetentionDaily is how many complete days to keep.
	RetentionDaily int
	// Timeout bounds one whole run.
	Timeout time.Duration
	// Disabled pauses backups while keeping the destination stored. Only the
	// Settings row sets it; the environment turns backups on with a bucket.
	Disabled bool
}

// Enabled reports whether backups are configured and not paused.
func (b BackupConfig) Enabled() bool { return !b.Disabled && strings.TrimSpace(b.Bucket) != "" }

// DailySchedule is a wall-clock time of day in a zone.
type DailySchedule struct {
	Hour, Minute int
	Location     *time.Location
}

// String renders the schedule the way BACKUP_SCHEDULE spells it.
func (s DailySchedule) String() string {
	return fmt.Sprintf("%02d:%02d %s", s.Hour, s.Minute, s.Location.String())
}

// Next returns the first run time strictly after t. On a DST spring-forward
// day the wall-clock time can fall in the gap; the run then happens at the
// first instant after the gap (02:00 Chicago becomes 03:00 CDT).
func (s DailySchedule) Next(t time.Time) time.Time {
	local := t.In(s.Location)
	next := s.on(local.Year(), local.Month(), local.Day())
	if !next.After(t) {
		next = s.on(local.Year(), local.Month(), local.Day()+1)
	}
	return next
}

// on returns the scheduled time on one calendar day.
func (s DailySchedule) on(year int, month time.Month, day int) time.Time {
	at := time.Date(year, month, day, s.Hour, s.Minute, 0, 0, s.Location)
	if at.Hour() != s.Hour || at.Minute() != s.Minute {
		// time.Date resolved the gap with the pre-gap offset, which lands an
		// hour early on the wall clock. Step to the first instant after it.
		at = at.Add(time.Hour)
	}
	return at
}

// ParseDailySchedule parses "HH:MM <IANA zone>". The zone is required so a
// container's UTC default never moves the run silently. It returns ok=false
// for "off".
func ParseDailySchedule(raw string) (sched DailySchedule, ok bool, err error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, BackupScheduleOff) {
		return DailySchedule{}, false, nil
	}
	fields := strings.Fields(raw)
	if len(fields) != 2 {
		return DailySchedule{}, false, fmt.Errorf("BACKUP_SCHEDULE must be \"HH:MM Zone/Name\" or \"off\", got %q", raw)
	}
	hh, mm, found := strings.Cut(fields[0], ":")
	if !found || len(hh) != 2 || len(mm) != 2 {
		return DailySchedule{}, false, fmt.Errorf("BACKUP_SCHEDULE time must be HH:MM, got %q", fields[0])
	}
	hour, herr := strconv.Atoi(hh)
	minute, merr := strconv.Atoi(mm)
	if herr != nil || merr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return DailySchedule{}, false, fmt.Errorf("BACKUP_SCHEDULE time must be HH:MM, got %q", fields[0])
	}
	loc, lerr := time.LoadLocation(fields[1])
	if lerr != nil {
		return DailySchedule{}, false, fmt.Errorf("BACKUP_SCHEDULE zone %q: %w", fields[1], lerr)
	}
	return DailySchedule{Hour: hour, Minute: minute, Location: loc}, true, nil
}

// Endpoint problems, shared by the env check (EndpointHost) and the Settings
// form check (ValidateSettings) so both accept the same URLs.
var (
	errEndpointMissing  = errors.New("endpoint missing")
	errEndpointURL      = errors.New("endpoint not a URL")
	errEndpointUserinfo = errors.New("endpoint has userinfo")
	errEndpointPath     = errors.New("endpoint has path")
	errEndpointScheme   = errors.New("endpoint scheme")
	errEndpointNoHost   = errors.New("endpoint has no host")
)

// parseEndpoint splits an endpoint URL into host[:port] and whether to use
// TLS. A missing scheme means https. It returns one of the errEndpoint*
// values and, for errEndpointScheme, the scheme it saw.
func parseEndpoint(raw string) (host string, secure bool, scheme string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, "", errEndpointMissing
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, perr := url.Parse(raw)
	if perr != nil {
		return "", false, "", errEndpointURL
	}
	if u.User != nil {
		return "", false, "", errEndpointUserinfo
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, "", errEndpointPath
	}
	switch u.Scheme {
	case "https":
		secure = true
	case "http":
		secure = false
	default:
		return "", false, u.Scheme, errEndpointScheme
	}
	if u.Host == "" {
		return "", false, "", errEndpointNoHost
	}
	return u.Host, secure, "", nil
}

// EndpointHost splits Endpoint into host[:port] and whether to use TLS.
func (b BackupConfig) EndpointHost() (host string, secure bool, err error) {
	host, secure, scheme, err := parseEndpoint(b.Endpoint)
	switch {
	case err == nil:
		return host, secure, nil
	case errors.Is(err, errEndpointMissing):
		return "", false, errors.New("BACKUP_S3_ENDPOINT is required when BACKUP_S3_BUCKET is set")
	case errors.Is(err, errEndpointUserinfo):
		return "", false, errors.New("BACKUP_S3_ENDPOINT must not contain credentials; use BACKUP_S3_ACCESS_KEY_ID and BACKUP_S3_SECRET_ACCESS_KEY")
	case errors.Is(err, errEndpointPath):
		return "", false, errors.New("BACKUP_S3_ENDPOINT must not contain a path; put the bucket in BACKUP_S3_BUCKET")
	case errors.Is(err, errEndpointScheme):
		return "", false, fmt.Errorf("BACKUP_S3_ENDPOINT scheme must be http or https, got %q", scheme)
	case errors.Is(err, errEndpointNoHost):
		return "", false, errors.New("BACKUP_S3_ENDPOINT has no host")
	default:
		return "", false, errors.New("BACKUP_S3_ENDPOINT is not a valid URL")
	}
}

func loadBackupConfig() BackupConfig {
	prefix := getEnv("BACKUP_S3_PREFIX", DefaultBackupPrefix)
	return BackupConfig{
		Endpoint:        strings.TrimSpace(getEnv("BACKUP_S3_ENDPOINT", "")),
		Region:          getEnv("BACKUP_S3_REGION", DefaultBackupRegion),
		Bucket:          strings.TrimSpace(getEnv("BACKUP_S3_BUCKET", "")),
		Prefix:          NormalizeBackupPrefix(prefix),
		AccessKeyID:     getEnv("BACKUP_S3_ACCESS_KEY_ID", ""),
		SecretAccessKey: getEnv("BACKUP_S3_SECRET_ACCESS_KEY", ""),
		ForcePathStyle:  getEnvBool("BACKUP_S3_FORCE_PATH_STYLE", false),
		Schedule:        getEnv("BACKUP_SCHEDULE", DefaultBackupSchedule),
		RetentionDaily:  getEnvInt("BACKUP_RETENTION_DAILY", DefaultBackupRetention),
		Timeout:         getEnvDuration("BACKUP_TIMEOUT", DefaultBackupTimeout),
	}
}

// NormalizeBackupPrefix trims slashes and whitespace and ends a non-empty
// prefix with exactly one slash.
func NormalizeBackupPrefix(p string) string {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// validate checks the backup settings. Nothing is checked while backups are
// disabled, so an unset bucket can never block startup.
func (b BackupConfig) validate() error {
	if !b.Enabled() {
		return nil
	}
	if _, _, err := b.EndpointHost(); err != nil {
		return err
	}
	if b.AccessKeyID == "" || b.SecretAccessKey == "" {
		return errors.New("BACKUP_S3_ACCESS_KEY_ID and BACKUP_S3_SECRET_ACCESS_KEY are required when BACKUP_S3_BUCKET is set")
	}
	if strings.ContainsAny(b.Prefix, "\\") || strings.Contains(b.Prefix, "..") {
		return errors.New(`BACKUP_S3_PREFIX must not contain backslashes or ".." segments`)
	}
	if b.RetentionDaily < 1 || b.RetentionDaily > 366 {
		return fmt.Errorf("BACKUP_RETENTION_DAILY must be between 1 and 366, got %d", b.RetentionDaily)
	}
	if b.Timeout <= 0 {
		return errors.New("BACKUP_TIMEOUT must be positive")
	}
	if _, _, err := ParseDailySchedule(b.Schedule); err != nil {
		return err
	}
	return nil
}

// BackupFieldError is a Settings form validation error. Field is the form
// field name (endpoint, bucket, secret_access_key, ...). Message never
// contains a submitted value, so it is safe to show and log.
type BackupFieldError struct {
	Field   string
	Message string
}

func (e *BackupFieldError) Error() string { return e.Field + ": " + e.Message }

func fieldErr(field, msg string) error { return &BackupFieldError{Field: field, Message: msg} }

// ValidateSettings checks settings saved from Settings → Profile with the
// same rules validate applies to BACKUP_*: endpoint URL shape, both keys,
// prefix, retention 1-366, a positive timeout, and the schedule. While
// Disabled is set the destination fields may be empty, but anything entered
// must still be well formed, so turning backups back on cannot surprise.
// Errors are *BackupFieldError.
func (b BackupConfig) ValidateSettings() error {
	enabled := !b.Disabled
	bucket := strings.TrimSpace(b.Bucket)
	if enabled && bucket == "" {
		return fieldErr("bucket", "is required when backups are enabled")
	}
	if strings.ContainsAny(bucket, "/\\ \t") {
		return fieldErr("bucket", "must be a bucket name, without slashes or spaces; put folders in the prefix")
	}
	if enabled || strings.TrimSpace(b.Endpoint) != "" {
		if _, _, scheme, err := parseEndpoint(b.Endpoint); err != nil {
			switch {
			case errors.Is(err, errEndpointMissing):
				return fieldErr("endpoint", "is required when backups are enabled")
			case errors.Is(err, errEndpointUserinfo):
				return fieldErr("endpoint", "must not contain credentials; use the access key fields")
			case errors.Is(err, errEndpointPath):
				return fieldErr("endpoint", "must not contain a path; put the bucket in the bucket field")
			case errors.Is(err, errEndpointScheme):
				return fieldErr("endpoint", fmt.Sprintf("scheme must be http or https, got %q", scheme))
			case errors.Is(err, errEndpointNoHost):
				return fieldErr("endpoint", "has no host")
			default:
				return fieldErr("endpoint", "is not a valid URL")
			}
		}
	}
	if strings.TrimSpace(b.Region) == "" {
		return fieldErr("region", "is required (use auto for Cloudflare R2)")
	}
	if enabled && b.AccessKeyID == "" {
		return fieldErr("access_key_id", "is required when backups are enabled")
	}
	if enabled && b.SecretAccessKey == "" {
		return fieldErr("secret_access_key", "is required when backups are enabled")
	}
	if strings.ContainsAny(b.Prefix, "\\") || strings.Contains(b.Prefix, "..") {
		return fieldErr("prefix", `must not contain backslashes or ".." segments`)
	}
	if b.RetentionDaily < 1 || b.RetentionDaily > 366 {
		return fieldErr("retention_daily", fmt.Sprintf("must be between 1 and 366, got %d", b.RetentionDaily))
	}
	if b.Timeout < time.Second {
		return fieldErr("timeout", "must be at least 1s")
	}
	if _, _, err := ParseDailySchedule(b.Schedule); err != nil {
		return fieldErr("schedule", strings.TrimPrefix(err.Error(), "BACKUP_SCHEDULE "))
	}
	return nil
}
