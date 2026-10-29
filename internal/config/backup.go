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
// empty. The access key and secret are never logged or rendered.
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
}

// Enabled reports whether backups are configured.
func (b BackupConfig) Enabled() bool { return strings.TrimSpace(b.Bucket) != "" }

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

// EndpointHost splits Endpoint into host[:port] and whether to use TLS.
func (b BackupConfig) EndpointHost() (host string, secure bool, err error) {
	raw := strings.TrimSpace(b.Endpoint)
	if raw == "" {
		return "", false, errors.New("BACKUP_S3_ENDPOINT is required when BACKUP_S3_BUCKET is set")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, errors.New("BACKUP_S3_ENDPOINT is not a valid URL")
	}
	if u.User != nil {
		return "", false, errors.New("BACKUP_S3_ENDPOINT must not contain credentials; use BACKUP_S3_ACCESS_KEY_ID and BACKUP_S3_SECRET_ACCESS_KEY")
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, errors.New("BACKUP_S3_ENDPOINT must not contain a path; put the bucket in BACKUP_S3_BUCKET")
	}
	switch u.Scheme {
	case "https":
		secure = true
	case "http":
		secure = false
	default:
		return "", false, fmt.Errorf("BACKUP_S3_ENDPOINT scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", false, errors.New("BACKUP_S3_ENDPOINT has no host")
	}
	return u.Host, secure, nil
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
