package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
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
	// AuditActionNotifySettingsUpdate records a save of the admin
	// Notifications form. Details hold {"changed": [field names]}, never
	// the URL or token.
	AuditActionNotifySettingsUpdate = "notify.settings.update"
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
	ResourceTypeNotifySettings  = "notify_settings"
)

// Audit sources. Source classifies a row by who made the change; the
// activity page filters on it (repositories.AuditFilter.Source uses the same
// rule in SQL).
const (
	AuditSourceMCP = "mcp"
	AuditSourceAPI = "api"
	AuditSourceUI  = "ui"
	// AuditSourceAgent matches MCP and API rows together (filter only).
	AuditSourceAgent = "agent"
)

// AuditMCPUserAgentPrefix starts the User-Agent of every MCP call: the stdio
// server sends "goforge-mcp" and /mcp sends "goforge-mcp-http (...)".
const AuditMCPUserAgentPrefix = "goforge-mcp"

// AuditAPIActionPrefix starts every API v1 action (api_v1.<tool>,
// api_v1.<tool>.completed, api_v1.<tool>.failed).
const AuditAPIActionPrefix = "api_v1."

// Source reports who made the change: MCP when the User-Agent is an MCP
// client, API for other api_v1 rows (or rows whose details.via is api_v1,
// such as a background run started by the API), and UI for everything else.
func (a *AuditLog) Source() string {
	if a.UserAgent.Valid && strings.HasPrefix(a.UserAgent.String, AuditMCPUserAgentPrefix) {
		return AuditSourceMCP
	}
	if strings.HasPrefix(a.Action, AuditAPIActionPrefix) {
		return AuditSourceAPI
	}
	if d, err := a.GetDetails(); err == nil && d["via"] == AuditViaAPI {
		return AuditSourceAPI
	}
	return AuditSourceUI
}

// Values of details.via.
const (
	AuditViaAPI = "api_v1"
	AuditViaUI  = "ui"
)

// Failed reports whether the row records a failed change (<action>.failed).
func (a *AuditLog) Failed() bool { return strings.HasSuffix(a.Action, ".failed") }

// UI audit actions. A UI mutation writes one row after it succeeds, with
// details.via "ui". A change that goes on in the background (a queued
// deployment, a service deploy or recreate) later writes
// deployment.run.<result> or service.run.<result> with details.audit_id set
// to this row's id.
const (
	AuditActionProjectCreate         = "project.create"
	AuditActionProjectUpdate         = "project.update"
	AuditActionProjectSettingsUpdate = "project.settings.update"
	AuditActionProjectDelete         = "project.delete"
	AuditActionProjectLinkService    = "project.link_service"
	AuditActionProjectUnlinkService  = "project.unlink_service"
	AuditActionDeploymentTrigger     = "deployment.trigger"
	AuditActionDeploymentRollback    = "deployment.rollback"
	AuditActionDeploymentCancel      = "deployment.cancel"
	AuditActionEnvironmentCreate     = "environment.create"
	AuditActionEnvironmentUpdate     = "environment.update"
	AuditActionEnvVarSet             = "env_var.set"
	AuditActionEnvVarDelete          = "env_var.delete"
	AuditActionEnvVarSync            = "env_var.sync"
	AuditActionServiceDeploy         = "service.deploy"
	AuditActionServiceDelete         = "service.delete"
	AuditActionServiceRestart        = "service.restart"
	AuditActionServiceUpdateConfig   = "service.update_config"
	AuditActionServiceUpdateImage    = "service.update_image"
	AuditActionServiceSetProtected   = "service.set_protected"
	AuditActionContainerStop         = "container.stop"
	AuditActionContainerRestart      = "container.restart"
)

// Background run outcomes, written when a queued deployment or a service
// deploy or recreate ends. details.audit_id is the row that started it.
const (
	AuditRunDeployment = "deployment.run"
	AuditRunService    = "service.run"

	AuditRunCompleted = "completed"
	AuditRunFailed    = "failed"
	AuditRunCanceled  = "canceled"
)

// ResourceTypeContainer is a Docker container; its id is not a UUID, so it
// is stored in details.resource_ref.
const ResourceTypeContainer = "container"

type auditIDKey struct{}

// WithAuditID returns ctx carrying the id of the audit row that approved or
// recorded the change being started. The deployment queue and the service
// instance background work read it so their final outcome row links back.
func WithAuditID(ctx context.Context, auditID string) context.Context {
	if auditID == "" {
		return ctx
	}
	return context.WithValue(ctx, auditIDKey{}, auditID)
}

// AuditIDFrom returns the audit id set by WithAuditID, or "".
func AuditIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(auditIDKey{}).(string); ok {
		return id
	}
	return ""
}
