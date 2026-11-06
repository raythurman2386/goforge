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
	// AuditActionErrorReportingSettingsUpdate records a save of the admin
	// Error reporting (Sentry) form. Details hold {"changed": [field
	// names]}, never the DSN.
	AuditActionErrorReportingSettingsUpdate = "error_reporting.settings.update"
	// AuditActionUserPasswordChange records a successful password change in
	// Settings → Security. Details hold {"sessions_revoked": n, "via": "ui"}:
	// the number of the account's other sessions that were ended. Never the
	// password or its hash.
	AuditActionUserPasswordChange = "user.password.change"
	// AuditActionUserSessionsRevoke records Settings → Security "Sign out
	// other sessions". Details hold {"sessions_revoked": n, "via": "ui"}.
	AuditActionUserSessionsRevoke = "user.sessions.revoke"
	// AuditActionUserProfileUpdate records a save of the Settings → Profile
	// form. Details hold {"changed": [field names], "old_email_domain",
	// "new_email_domain", "via": "ui"}: the domain part only, never the full
	// address.
	AuditActionUserProfileUpdate = "user.profile.update"
	// AuditActionGitSourceCreate and AuditActionGitSourceDelete record
	// Settings → Git sources changes. Details hold the name, the type, and
	// whether an access token or API URL was set, never the token, key, or
	// URL.
	AuditActionGitSourceCreate = "git_source.create"
	AuditActionGitSourceDelete = "git_source.delete"
	// AuditActionUser2FAEnable records a confirmed TOTP enrollment in
	// Settings → Security. Details hold {"recovery_codes": n, "via": "ui"},
	// never the secret or a code.
	AuditActionUser2FAEnable = "user.2fa.enable"
	// AuditActionUser2FADisable records Settings → Security turning TOTP off
	// with the password and a code. Details hold {"second_factor":
	// "totp"|"recovery_code", "via": "ui"}.
	AuditActionUser2FADisable = "user.2fa.disable"
	// AuditActionUser2FAReset is written by `goforge user reset-2fa` (lockout
	// recovery from the host). Details hold {"via": "cli"}.
	AuditActionUser2FAReset = "user.2fa.reset"
	// AuditActionLogin2FAFailed records a wrong TOTP or recovery code on the
	// sign-in step after a correct password. Details hold {"attempt": n,
	// "challenge_ended": bool}, never the code. A successful second step
	// writes the usual login row with details.second_factor.
	AuditActionLogin2FAFailed = "login.2fa_failed"
	// AuditActionAuditSettingsUpdate records an admin save of audit log
	// retention. Details hold {"old_days": n, "new_days": n}.
	AuditActionAuditSettingsUpdate = "audit.settings.update"
	// AuditActionAuditRetentionPurge records one retention run that deleted
	// rows. Details hold {"deleted", "cutoff", "through_seq", "through_hash"}.
	AuditActionAuditRetentionPurge = "audit.retention.purge"
)

// Values of details.second_factor on login and user.2fa.disable rows.
const (
	SecondFactorTOTP         = "totp"
	SecondFactorRecoveryCode = "recovery_code"
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
	// ResourceTypeErrorReportingSettings is the Sentry settings row.
	ResourceTypeErrorReportingSettings = "error_reporting_settings"
	// ResourceTypeAuditSettings is the audit retention settings row.
	ResourceTypeAuditSettings = "audit_settings"
)

// Audit sources. Source classifies a row by who made the change; the
// activity page filters on it (repositories.AuditFilter.Source uses the same
// rule in SQL).
const (
	AuditSourceMCP = "mcp"
	AuditSourceAPI = "api"
	AuditSourceUI  = "ui"
	// AuditSourceWebhook is a row written for a signed push webhook
	// (details.via "webhook") and the run outcomes linked to it.
	AuditSourceWebhook = "webhook"
	// AuditSourceCLI is a row written by a goforge host command
	// (details.via "cli"), such as user reset-2fa.
	AuditSourceCLI = "cli"
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
// such as a background run started by the API), webhook for rows whose
// details.via is webhook, CLI for rows whose details.via is cli, and UI for
// everything else.
func (a *AuditLog) Source() string {
	if a.UserAgent.Valid && strings.HasPrefix(a.UserAgent.String, AuditMCPUserAgentPrefix) {
		return AuditSourceMCP
	}
	if strings.HasPrefix(a.Action, AuditAPIActionPrefix) {
		return AuditSourceAPI
	}
	if d, err := a.GetDetails(); err == nil {
		switch d["via"] {
		case AuditViaAPI:
			return AuditSourceAPI
		case AuditViaWebhook:
			return AuditSourceWebhook
		case AuditViaCLI:
			return AuditSourceCLI
		}
	}
	return AuditSourceUI
}

// Values of details.via.
const (
	AuditViaAPI = "api_v1"
	AuditViaUI  = "ui"
	// AuditViaWebhook marks a deployment.trigger row written for a signed
	// push webhook. user_id is the project owner; details carry the
	// provider, branch, and commit.
	AuditViaWebhook = "webhook"
	// AuditViaCLI marks a row written by a goforge host command, such as
	// user reset-2fa.
	AuditViaCLI = "cli"
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
	AuditActionServiceSetBackup      = "service.set_backup"
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
