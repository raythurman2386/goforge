package models

import (
	"time"
)

// EnvVariable represents an environment variable for a deployment environment.
type EnvVariable struct {
	ID            string
	EnvironmentID string
	Key           string
	Value         string // encrypted if IsSecret
	IsSecret      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
