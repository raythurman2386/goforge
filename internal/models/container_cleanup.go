package models

import (
	"database/sql"
	"time"
)

// ContainerCleanup is a durable host-removal intent. The container row and the
// intent are removed only after Docker confirms the host object is gone.
type ContainerCleanup struct {
	DockerID      string
	ProjectID     sql.NullString
	EnvironmentID sql.NullString
	Reason        string
	CreatedAt     time.Time
}

// Durable cleanup provenance; only automatic discoveries emit orphan audits.
const (
	ContainerCleanupDelete        = "delete"
	ContainerCleanupOrphan        = "orphan"
	ContainerCleanupCreateFailure = "create_failure"
)
