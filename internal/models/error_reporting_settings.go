package models

import (
	"database/sql"
	"time"
)

// ErrorReportingSettings is the one error_reporting_settings row an admin
// saves in Settings → Profile → Error reporting. The repository seals DSN
// with ENCRYPTION_KEY; in this struct it is plaintext. A blank DSN means
// error reporting is off. A blank Environment means automatic.
type ErrorReportingSettings struct {
	DSN         string
	Environment string
	UpdatedBy   sql.NullString
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
