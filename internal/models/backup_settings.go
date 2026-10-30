package models

import (
	"database/sql"
	"time"
)

// BackupSettings is the one backup_settings row an admin saves in Settings →
// Profile. Once it exists it wins over the BACKUP_* environment. The
// repository seals AccessKeyID and SecretAccessKey with ENCRYPTION_KEY; in
// this struct they are plaintext.
type BackupSettings struct {
	Enabled         bool
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
	Schedule        string
	RetentionDaily  int
	Timeout         time.Duration
	UpdatedBy       sql.NullString
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
