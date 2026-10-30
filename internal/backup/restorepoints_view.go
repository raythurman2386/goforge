package backup

import (
	"context"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
)

// RestorePointsView is the read-only Restore points list on the Settings →
// Profile Backups card. Restoring is CLI-only (goforge backup restore).
type RestorePointsView struct {
	Bucket, Prefix string
	// Schedule is "HH:MM <zone>", used to show times in the schedule's zone.
	Schedule   string
	Points     []RestorePoint
	Incomplete int
	// Error is a redacted reason the list is unavailable, or "".
	Error string
}

// RestorePointLister reads restore points with the live backup settings
// (Manager.Current). It only lists keys and reads manifest.json files.
type RestorePointLister struct {
	Current  func() (config.BackupConfig, string)
	Redactor func(extra ...config.BackupConfig) *Redactor
	// NewStore defaults to NewS3Store.
	NewStore func(config.BackupConfig) (Store, error)
	// Limit is how many points to show (default 10).
	Limit int
	// Timeout bounds the listing (default 10s).
	Timeout time.Duration
}

// List returns the newest restore points. It never returns a secret: errors
// are redacted with every key in play.
func (l *RestorePointLister) List(ctx context.Context) *RestorePointsView {
	cfg, _ := l.Current()
	v := &RestorePointsView{Bucket: cfg.Bucket, Prefix: cfg.Prefix, Schedule: cfg.Schedule}
	if strings.TrimSpace(cfg.Bucket) == "" {
		v.Error = "No backup bucket is configured."
		return v
	}
	red := NewRedactor()
	if l.Redactor != nil {
		red = l.Redactor(cfg)
	}
	newStore := l.NewStore
	if newStore == nil {
		newStore = func(c config.BackupConfig) (Store, error) { return NewS3Store(c) }
	}
	limit, timeout := l.Limit, l.Timeout
	if limit <= 0 {
		limit = 10
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	store, err := newStore(cfg)
	if err != nil {
		v.Error = "Could not open the bucket: " + red.Redact(err.Error())
		return v
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	points, incomplete, err := RestorePoints(ctx, store, cfg.Prefix, limit)
	if err != nil {
		v.Error = "Could not list the bucket: " + red.Redact(err.Error())
		return v
	}
	v.Points, v.Incomplete = points, incomplete
	return v
}
