package backup

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
)

// ErrDisabled is returned when backups are off: no bucket in the effective
// settings, or an admin turned them off in Settings.
var ErrDisabled = errors.New("backups are disabled: turn them on in Settings → Profile or set BACKUP_S3_BUCKET (see docs/deployment/backup.md)")

// Setup is everything serve and the CLI need to run backups.
type Setup struct {
	Runner *Runner
	// Schedule is valid only when Scheduled is true (BACKUP_SCHEDULE is not
	// "off").
	Schedule  config.DailySchedule
	Scheduled bool
}

// RedactorFor builds a Redactor that knows every GoForge secret: the app
// secrets, the BACKUP_* keys from the environment, and the keys of each extra
// backup config (the Settings row, which can differ from the environment).
func RedactorFor(cfg *config.Config, extra ...config.BackupConfig) *Redactor {
	secrets := []string{
		cfg.EncryptionKey, cfg.SessionSecret, cfg.CSRFSecret, cfg.WebhookSecret, cfg.GitHubClientSecret,
		cfg.Backup.AccessKeyID, cfg.Backup.SecretAccessKey,
	}
	for i := range extra {
		secrets = append(secrets, extra[i].AccessKeyID, extra[i].SecretAccessKey)
	}
	if u, err := url.Parse(cfg.DatabaseURL); err == nil && u.User != nil {
		if p, ok := u.User.Password(); ok {
			secrets = append(secrets, p)
		}
	}
	return NewRedactor(secrets...)
}

// New wires a Runner from the environment backup config. It returns
// ErrDisabled when backups are off.
func New(cfg *config.Config, db *sql.DB, dockerClient *docker.Client, log *slog.Logger, version string) (*Setup, error) {
	return NewFor(cfg, cfg.Backup, db, dockerClient, log, version)
}

// NewFor wires a Runner for bc, the effective backup config (see
// Effective). cfg supplies the app database URL and the secrets to redact.
// It returns ErrDisabled when bc is off.
func NewFor(cfg *config.Config, bc config.BackupConfig, db *sql.DB, dockerClient *docker.Client, log *slog.Logger, version string) (*Setup, error) {
	if !bc.Enabled() {
		return nil, ErrDisabled
	}
	store, err := NewS3Store(bc)
	if err != nil {
		return nil, err
	}
	sched, scheduled, err := config.ParseDailySchedule(bc.Schedule)
	if err != nil {
		return nil, err
	}
	loc := time.UTC
	if scheduled {
		loc = sched.Location
	}
	red := RedactorFor(cfg, bc)
	return &Setup{
		Runner: &Runner{
			Store: store,
			Runs:  repositories.NewBackupRunRepository(db),
			Sources: []Source{
				Static{&AppDBTarget{DatabaseURL: cfg.DatabaseURL, Redactor: red}},
				&InstanceSource{
					Instances: repositories.NewServiceInstanceRepository(db),
					Docker:    DockerExecer{Client: dockerClient},
					Redactor:  red,
				},
			},
			Prefix:    bc.Prefix,
			Retention: bc.RetentionDaily,
			Location:  loc,
			Timeout:   bc.Timeout,
			Version:   version,
			Redactor:  red,
			Logger:    log,
		},
		Schedule:  sched,
		Scheduled: scheduled,
	}, nil
}
