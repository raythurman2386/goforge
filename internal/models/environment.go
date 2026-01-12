package models

import (
	"database/sql"
	"time"
)

// Environment represents a deployment environment (dev, staging, prod, etc).
type Environment struct {
	ID          string
	ProjectID   string
	Name        string
	Domain      sql.NullString
	Port        sql.NullInt32
	Branch      sql.NullString // override project default
	Replicas    int
	CPULimit    sql.NullString
	MemoryLimit sql.NullString
	AutoDeploy  bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
