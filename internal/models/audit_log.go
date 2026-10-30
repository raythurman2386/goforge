package models

import (
	"database/sql"
	"encoding/json"
	"time"
)

// AuditLog represents an audit log entry for tracking user actions.
type AuditLog struct {
	ID           string
	UserID       sql.NullString
	Action       string
	ResourceType string
	ResourceID   sql.NullString
	Details      json.RawMessage
	IPAddress    sql.NullString
	UserAgent    sql.NullString
	CreatedAt    time.Time
}

// GetDetails parses the details JSON into a map.
func (a *AuditLog) GetDetails() (map[string]interface{}, error) {
	if len(a.Details) == 0 {
		return make(map[string]interface{}), nil
	}
	var details map[string]interface{}
	if err := json.Unmarshal(a.Details, &details); err != nil {
		return nil, err
	}
	return details, nil
}

// Common audit actions
const (
	AuditActionCreate = "create"
	AuditActionUpdate = "update"
	AuditActionDelete = "delete"
	AuditActionDeploy = "deploy"
	AuditActionLogin  = "login"
	AuditActionLogout = "logout"
	// AuditActionBackupSettingsUpdate records a save of the admin Backups
	// form. Details hold {"changed": [field names]}, never values.
	AuditActionBackupSettingsUpdate = "backup.settings.update"
	// AuditActionAPITokenCreate, AuditActionAPITokenRotate, and
	// AuditActionAPITokenRevoke record Profile API token changes. Details
	// carry the name, scopes, and expiry, never the token or its hint.
	AuditActionAPITokenCreate = "api_token.create"
	AuditActionAPITokenRotate = "api_token.rotate"
	AuditActionAPITokenRevoke = "api_token.revoke"
	// AuditActionBackupRestore is written by `goforge backup restore` into
	// the restored database. Details hold {"run_key": ...} only.
	AuditActionBackupRestore = "backup.restore"
)

// Common resource types
const (
	ResourceTypeUser            = "user"
	ResourceTypeProject         = "project"
	ResourceTypeEnvironment     = "environment"
	ResourceTypeDeployment      = "deployment"
	ResourceTypeServiceInstance = "service_instance"
	ResourceTypeGitSource       = "git_source"
	ResourceTypeBackupSettings  = "backup_settings"
	ResourceTypeAPIToken        = "api_token"
	ResourceTypeBackupRun       = "backup_run"
)
