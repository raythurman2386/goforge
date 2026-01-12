package models

import (
	"database/sql"
	"time"
)

// DeploymentStatus represents the status of a deployment.
type DeploymentStatus string

const (
	DeploymentStatusPending    DeploymentStatus = "pending"
	DeploymentStatusCloning    DeploymentStatus = "cloning"
	DeploymentStatusBuilding   DeploymentStatus = "building"
	DeploymentStatusDeploying  DeploymentStatus = "deploying"
	DeploymentStatusRunning    DeploymentStatus = "running"
	DeploymentStatusFailed     DeploymentStatus = "failed"
	DeploymentStatusRolledBack DeploymentStatus = "rolled_back"
	DeploymentStatusCancelled  DeploymentStatus = "cancelled"
)

// TriggerType represents what triggered the deployment.
type TriggerType string

const (
	TriggerTypeManual   TriggerType = "manual"
	TriggerTypeWebhook  TriggerType = "webhook"
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
		DeploymentStatusRolledBack, DeploymentStatusCancelled:
		return true
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
	LogLevelDebug DeploymentLogLevel = "debug"
	LogLevelInfo  DeploymentLogLevel = "info"
	LogLevelWarn  DeploymentLogLevel = "warn"
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
