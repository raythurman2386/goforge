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

// ErrDisabled is returned when BACKUP_S3_BUCKET is unset.
var ErrDisabled = errors.New("backups are disabled: set BACKUP_S3_BUCKET (see docs/deployment/backup.md)")

// Setup is everything serve and the CLI need to run backups.
type Setup struct {
	Runner *Runner
	// Schedule is valid only when Scheduled is true (BACKUP_SCHEDULE is not
	// "off").
	Schedule  config.DailySchedule
	Scheduled bool
}

// RedactorFor builds a Redactor that knows every GoForge secret.
func RedactorFor(cfg *config.Config) *Redactor {
	secrets := []string{
		cfg.EncryptionKey, cfg.SessionSecret, cfg.CSRFSecret, cfg.WebhookSecret, cfg.GitHubClientSecret,
		cfg.Backup.AccessKeyID, cfg.Backup.SecretAccessKey,
	}
	if u, err := url.Parse(cfg.DatabaseURL); err == nil && u.User != nil {
		if p, ok := u.User.Password(); ok {
			secrets = append(secrets, p)
		}
	}
	return NewRedactor(secrets...)
}

// New wires a Runner from config. It returns ErrDisabled when backups are off.
func New(cfg *config.Config, db *sql.DB, dockerClient *docker.Client, log *slog.Logger, version string) (*Setup, error) {
	if !cfg.Backup.Enabled() {
		return nil, ErrDisabled
	}
	store, err := NewS3Store(cfg.Backup)
	if err != nil {
		return nil, err
	}
	sched, scheduled, err := config.ParseDailySchedule(cfg.Backup.Schedule)
	if err != nil {
		return nil, err
	}
	loc := time.UTC
	if scheduled {
		loc = sched.Location
	}
	red := RedactorFor(cfg)
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
			Prefix:    cfg.Backup.Prefix,
			Retention: cfg.Backup.RetentionDaily,
			Location:  loc,
			Timeout:   cfg.Backup.Timeout,
			Version:   version,
			Redactor:  red,
			Logger:    log,
		},
		Schedule:  sched,
		Scheduled: scheduled,
	}, nil
}
