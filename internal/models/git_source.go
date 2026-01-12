package models

import (
	"database/sql"
	"time"
)

// GitSourceType represents the type of git provider.
type GitSourceType string

const (
	GitSourceTypeGitHub GitSourceType = "github"
	GitSourceTypeGitLab GitSourceType = "gitlab"
	GitSourceTypeGitea  GitSourceType = "gitea"
	GitSourceTypeSSH    GitSourceType = "ssh"
)

// GitSource represents a configured git provider.
type GitSource struct {
	ID            string
	UserID        string
	Name          string
	Type          GitSourceType
	APIURL        sql.NullString // For self-hosted instances
	AccessToken   sql.NullString // encrypted
	SSHPrivateKey sql.NullString // encrypted
	IsDefault     bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
