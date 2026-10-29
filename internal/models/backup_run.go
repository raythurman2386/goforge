package models

import (
	"database/sql"
	"time"
)

// BackupRunStatus is the state of one backup run.
type BackupRunStatus string

const (
	// BackupRunRunning means the run has started and not finished.
	BackupRunRunning BackupRunStatus = "running"
	// BackupRunSucceeded means every artifact and the manifest uploaded.
	BackupRunSucceeded BackupRunStatus = "succeeded"
	// BackupRunFailed means at least one artifact or the manifest failed, or
	// the process stopped mid-run. A failed run never deletes old backups.
	BackupRunFailed BackupRunStatus = "failed"
)

// BackupTrigger says what started a run.
type BackupTrigger string

const (
	// BackupTriggerSchedule is the in-app nightly scheduler.
	BackupTriggerSchedule BackupTrigger = "schedule"
	// BackupTriggerManual is `goforge backup run`.
	BackupTriggerManual BackupTrigger = "manual"
)

// BackupRun is one row of backup_runs.
type BackupRun struct {
	ID      string
	Trigger BackupTrigger
	Status  BackupRunStatus
	// RunKey is the object path of the run under the bucket prefix,
	// <YYYY-MM-DD>/<YYYYMMDDTHHMMSSZ>.
	RunKey     string
	StartedAt  time.Time
	FinishedAt sql.NullTime
	Objects    int
	Bytes      int64
	Skipped    int
	// Pruned is how many objects retention deleted after this run.
	Pruned int
	// ErrorSummary is a short, redacted reason. Never a secret.
	ErrorSummary sql.NullString
}
