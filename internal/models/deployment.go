package models

import (
	"database/sql"
	"time"
)

// DeploymentStatus represents the status of a deployment.
type DeploymentStatus string

const (
	// DeploymentStatusPending indicates the deployment is waiting to start.
	DeploymentStatusPending DeploymentStatus = "pending"
	// DeploymentStatusCloning indicates the repository is being cloned.
	DeploymentStatusCloning DeploymentStatus = "cloning"
	// DeploymentStatusBuilding indicates the application is being built.
	DeploymentStatusBuilding DeploymentStatus = "building"
	// DeploymentStatusDeploying indicates the application is being deployed.
	DeploymentStatusDeploying DeploymentStatus = "deploying"
	// DeploymentStatusRunning indicates the deployment has finished successfully.
	DeploymentStatusRunning DeploymentStatus = "running"
	// DeploymentStatusFailed indicates the deployment has failed.
	DeploymentStatusFailed DeploymentStatus = "failed"
	// DeploymentStatusRolledBack indicates the deployment was rolled back.
	DeploymentStatusRolledBack DeploymentStatus = "rolled_back"
	// DeploymentStatusCanceled indicates the deployment was canceled.
	DeploymentStatusCanceled DeploymentStatus = "canceled"
)

// TriggerType represents what triggered the deployment.
type TriggerType string

const (
	// TriggerTypeManual indicates a manual deployment.
	TriggerTypeManual TriggerType = "manual"
	// TriggerTypeWebhook indicates a deployment triggered by a webhook.
	TriggerTypeWebhook TriggerType = "webhook"
	// TriggerTypeRollback indicates a deployment triggered by a rollback.
	TriggerTypeRollback TriggerType = "rollback"
)

// Deployment represents a deployment of an environment.
type Deployment struct {
	ID            string
	EnvironmentID string
	Status        DeploymentStatus
	TriggerType   TriggerType
	CommitSHA     sql.NullString
	CommitMessage sql.NullString
	CommitAuthor  sql.NullString
	Branch        sql.NullString
	ImageTag      sql.NullString
	ErrorMessage  sql.NullString
	StartedAt     sql.NullTime
	FinishedAt    sql.NullTime
	CreatedAt     time.Time
}

// IsTerminal returns true if the deployment is in a terminal state.
func (d *Deployment) IsTerminal() bool {
	switch d.Status {
	case DeploymentStatusRunning, DeploymentStatusFailed,
		DeploymentStatusRolledBack, DeploymentStatusCanceled:
		return true
	case DeploymentStatusPending, DeploymentStatusCloning,
		DeploymentStatusBuilding, DeploymentStatusDeploying:
		return false
	default:
		return false
	}
}

// Duration returns the duration of the deployment.
func (d *Deployment) Duration() time.Duration {
	if !d.StartedAt.Valid {
		return 0
	}
	end := time.Now()
	if d.FinishedAt.Valid {
		end = d.FinishedAt.Time
	}
	return end.Sub(d.StartedAt.Time)
}

// DeploymentLogLevel represents the severity of a log message.
type DeploymentLogLevel string

const (
	// LogLevelDebug indicates a debug message.
	LogLevelDebug DeploymentLogLevel = "debug"
	// LogLevelInfo indicates an informational message.
	LogLevelInfo DeploymentLogLevel = "info"
	// LogLevelWarn indicates a warning message.
	LogLevelWarn DeploymentLogLevel = "warn"
	// LogLevelError indicates an error message.
	LogLevelError DeploymentLogLevel = "error"
)

// DeploymentLog represents a log entry from a deployment.
type DeploymentLog struct {
	ID           string
	DeploymentID string
	Timestamp    time.Time
	Level        DeploymentLogLevel
	Message      string
	Source       sql.NullString // build, deploy, health_check, etc.
}

// QueueStatus represents the status of a queued deployment job.
type QueueStatus string

const (
	// QueueStatusPending indicates the job is waiting to be processed.
	QueueStatusPending QueueStatus = "pending"
	// QueueStatusProcessing indicates the job is currently being processed.
	QueueStatusProcessing QueueStatus = "processing"
	// QueueStatusCompleted indicates the job completed successfully.
	QueueStatusCompleted QueueStatus = "completed"
	// QueueStatusFailed indicates the job failed.
	QueueStatusFailed QueueStatus = "failed"
)

// QueueItem represents a deployment job in the queue.
type QueueItem struct {
	ID           string
	DeploymentID string
	Status       QueueStatus
	CreatedAt    time.Time
	StartedAt    sql.NullTime
	FinishedAt   sql.NullTime
	ErrorMessage sql.NullString
}

// QueueStats holds statistics about the deployment queue.
type QueueStats struct {
	Pending    int
	Processing int
	Completed  int
	Failed     int
}
