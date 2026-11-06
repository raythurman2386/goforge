package models

import (
	"database/sql"
	"time"
)

// AuditSettings is the single audit_settings row (id = 1).
// RetentionSaved is false until an admin saves Settings → Security, so a row
// that exists only to store the chain boundary does not override the
// environment. BoundarySeq and BoundaryHash are the prefix the retention
// purge has removed (seq 0 and 32 zero bytes until the first purge).
type AuditSettings struct {
	RetentionDays  int
	RetentionSaved bool
	BoundarySeq    int64
	BoundaryHash   []byte
	UpdatedBy      sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
