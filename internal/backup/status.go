package backup

import (
	"context"
	"errors"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// RunLister reads backup_runs for status.
type RunLister interface {
	List(ctx context.Context, limit int) ([]*models.BackupRun, error)
	LatestSucceeded(ctx context.Context) (*models.BackupRun, error)
}

// Status is the read-only backup status shown by the API, MCP, and Settings.
// It never carries credentials or the endpoint.
type Status struct {
	Enabled        bool       `json:"enabled"`
	Bucket         string     `json:"bucket,omitempty"`
	Prefix         string     `json:"prefix,omitempty"`
	Schedule       string     `json:"schedule"`
	RetentionDaily int        `json:"retention_daily"`
	NextRunAt      *time.Time `json:"next_run_at"`
	LastRun        *RunView   `json:"last_run"`
	LastSuccess    *RunView   `json:"last_success"`
	Recent         []RunView  `json:"recent"`
	// ConfiguredBy is "settings" (the row an admin saved) or "environment"
	// (BACKUP_*, no row yet).
	ConfiguredBy string `json:"configured_by,omitempty"`
}

// RunView is a backup_runs row as JSON.
type RunView struct {
	ID           string     `json:"id"`
	Trigger      string     `json:"trigger"`
	Status       string     `json:"status"`
	RunKey       string     `json:"run_key"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Objects      int        `json:"objects"`
	Bytes        int64      `json:"bytes"`
	Skipped      int        `json:"skipped"`
	Pruned       int        `json:"pruned"`
	ErrorSummary string     `json:"error_summary,omitempty"`
}

// NewRunView converts a row.
func NewRunView(r *models.BackupRun) *RunView {
	if r == nil {
		return nil
	}
	v := &RunView{
		ID: r.ID, Trigger: string(r.Trigger), Status: string(r.Status), RunKey: r.RunKey,
		StartedAt: r.StartedAt, Objects: r.Objects, Bytes: r.Bytes, Skipped: r.Skipped, Pruned: r.Pruned,
		ErrorSummary: r.ErrorSummary.String,
	}
	if r.FinishedAt.Valid {
		t := r.FinishedAt.Time
		v.FinishedAt = &t
	}
	return v
}

// LiveConfig is the config in effect right now (the Manager).
type LiveConfig interface {
	Current() (config.BackupConfig, string)
}

// StatusReader builds Status.
type StatusReader struct {
	// Config is used when Live is nil.
	Config config.BackupConfig
	// Live, when set, supplies the config in effect, so Settings saves show
	// up without a restart.
	Live LiveConfig
	Runs RunLister
	Now  func() time.Time
}

// Status reads the current backup status.
func (s *StatusReader) Status(ctx context.Context) (*Status, error) {
	cfg, source := s.Config, ""
	if s.Live != nil {
		cfg, source = s.Live.Current()
	}
	st := &Status{
		Enabled:        cfg.Enabled(),
		Schedule:       config.BackupScheduleOff,
		RetentionDaily: cfg.RetentionDaily,
		Recent:         []RunView{},
		ConfiguredBy:   source,
	}
	if st.Enabled {
		st.Bucket, st.Prefix = cfg.Bucket, cfg.Prefix
		if sched, ok, err := config.ParseDailySchedule(cfg.Schedule); err == nil && ok {
			st.Schedule = sched.String()
			now := time.Now
			if s.Now != nil {
				now = s.Now
			}
			next := sched.Next(now())
			st.NextRunAt = &next
		}
	} else {
		st.Schedule = "disabled"
	}
	if s.Runs == nil {
		return st, nil
	}
	recent, err := s.Runs.List(ctx, 10)
	if err != nil {
		return nil, err
	}
	for _, r := range recent {
		st.Recent = append(st.Recent, *NewRunView(r))
	}
	if len(recent) > 0 {
		st.LastRun = NewRunView(recent[0])
	}
	last, err := s.Runs.LatestSucceeded(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
	case err != nil:
		return nil, err
	default:
		st.LastSuccess = NewRunView(last)
	}
	return st, nil
}
