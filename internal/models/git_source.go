package models

import (
	"database/sql"
	"time"
)

// GitSourceType represents the type of git provider.
type GitSourceType string

const (
	// GitSourceTypeGitHub represents GitHub provider.
	GitSourceTypeGitHub GitSourceType = "github"
	// GitSourceTypeGitLab represents GitLab provider.
	GitSourceTypeGitLab GitSourceType = "gitlab"
	// GitSourceTypeGitea represents Gitea provider.
	GitSourceTypeGitea GitSourceType = "gitea"
	// GitSourceTypeSSH represents a raw git repository accessed via SSH.
	GitSourceTypeSSH GitSourceType = "ssh"
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
