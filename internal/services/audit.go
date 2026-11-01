package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// AuditLogRepository defines the interface for audit log data access.
type AuditLogRepository interface {
	Create(ctx context.Context, l *models.AuditLog) error
	List(ctx context.Context, limit, offset int) ([]*models.AuditLog, error)
	ListByResource(ctx context.Context, resourceType, resourceID string, limit, offset int) ([]*models.AuditLog, error)
}

// AuditService handles recording and retrieving audit logs.
type AuditService struct {
	repo   AuditLogRepository
	logger *slog.Logger
}

// NewAuditService creates a new AuditService.
func NewAuditService(repo AuditLogRepository, logger *slog.Logger) *AuditService {
	return &AuditService{
		repo:   repo,
		logger: logger,
	}
}

// Record records a new audit log entry. It writes in the background so the
// request does not wait; a failed insert is only logged.
func (s *AuditService) Record(ctx context.Context, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string) {
	entry, err := buildAuditEntry("", userID, action, resourceType, resourceID, details, ip, userAgent)
	if err != nil {
		s.logger.Error("failed to marshal audit details", "error", err)
		return
	}
	// Execute in background to not block the request
	//nolint:contextcheck // background task needs its own context
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.Create(bgCtx, entry); err != nil {
			s.logger.Error("failed to create audit log", "error", err)
		}
	}()
}

// RecordAs writes an audit row with a caller-chosen id (empty picks one)
// before it returns. A UI handler that starts background work (a queued
// deployment, a service deploy or recreate) picks the id, records the row,
// and passes the id on with models.WithAuditID, so the outcome row written
// when the work ends can read this row back. A failed insert is logged and
// does not block the change.
func (s *AuditService) RecordAs(ctx context.Context, id, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string) {
	entry, err := buildAuditEntry(id, userID, action, resourceType, resourceID, details, ip, userAgent)
	if err != nil {
		s.logger.Error("failed to marshal audit details", "error", err)
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repo.Create(wctx, entry); err != nil {
		s.logger.Error("failed to create audit log", "action", action, "error", err)
	}
}

// buildAuditEntry makes an audit row. A resource id that is not a UUID (a
// Docker container id) goes to details.resource_ref, since resource_id is a
// UUID column. The caller's details map is never modified.
func buildAuditEntry(id, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string) (*models.AuditLog, error) {
	if id == "" {
		id = uuid.New().String()
	}
	e := &models.AuditLog{ID: id, Action: action, ResourceType: resourceType, CreatedAt: time.Now()}
	if userID != "" {
		e.UserID = sql.NullString{String: userID, Valid: true}
	}
	if _, err := uuid.Parse(resourceID); err == nil {
		e.ResourceID = sql.NullString{String: resourceID, Valid: true}
	} else if resourceID != "" {
		withRef := make(map[string]interface{}, len(details)+1)
		for k, v := range details {
			withRef[k] = v
		}
		withRef["resource_ref"] = resourceID
		details = withRef
	}
	if ip != "" {
		e.IPAddress = sql.NullString{String: ip, Valid: true}
	}
	if userAgent != "" {
		e.UserAgent = sql.NullString{String: userAgent, Valid: true}
	}
	if details != nil {
		raw, err := json.Marshal(details)
		if err != nil {
			return nil, err
		}
		e.Details = raw
	}
	return e, nil
}

// List retrieves audit logs.
func (s *AuditService) List(ctx context.Context, page, pageSize int) ([]*models.AuditLog, error) {
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, pageSize, offset)
}

// ListByResource retrieves audit logs for a resource.
func (s *AuditService) ListByResource(ctx context.Context, resourceType, resourceID string, page, pageSize int) ([]*models.AuditLog, error) {
	offset := (page - 1) * pageSize
	return s.repo.ListByResource(ctx, resourceType, resourceID, pageSize, offset)
}

// AuditRunStore reads the audit row that started a background change and
// writes its outcome. *repositories.AuditLogRepository implements it.
type AuditRunStore interface {
	Create(ctx context.Context, l *models.AuditLog) error
	GetByID(ctx context.Context, id string) (*models.AuditLog, error)
}

// maxRunErrorLen caps the error kept in a run outcome row.
const maxRunErrorLen = 500

// runOutcomeTimeout bounds the lookup and insert of a run outcome row.
const runOutcomeTimeout = 10 * time.Second

// RunOutcome is the end of a background change: a queued deployment or a
// service deploy or recreate.
type RunOutcome struct {
	// Run is models.AuditRunDeployment or models.AuditRunService.
	Run          string
	ResourceType string
	ResourceID   string
	// Result is models.AuditRunCompleted, AuditRunFailed, or AuditRunCanceled.
	Result string
	// Error is the failure shown to the user (already redacted, as stored
	// in last_error or the deployment error). It is capped.
	Error string
	// Status is the final deployment or service status.
	Status string
}

// RecordRunOutcome writes <run>.<result> (for example deployment.run.failed)
// linked to the audit row auditID through details.audit_id. The row takes
// the user, address, and user agent of the row that started the change, and
// its via, approval_ref, token, and project, so the activity page shows it
// under the same source and token. It does nothing when store is nil or
// auditID is empty, and only logs when the insert fails: the change already
// happened.
func RecordRunOutcome(store AuditRunStore, logger *slog.Logger, auditID string, out RunOutcome) {
	if store == nil || auditID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runOutcomeTimeout)
	defer cancel()

	entry := &models.AuditLog{
		ID:           uuid.New().String(),
		Action:       out.Run + "." + out.Result,
		ResourceType: out.ResourceType,
		CreatedAt:    time.Now(),
	}
	details := map[string]interface{}{"audit_id": auditID}
	if _, err := uuid.Parse(out.ResourceID); err == nil {
		entry.ResourceID = sql.NullString{String: out.ResourceID, Valid: true}
	} else if out.ResourceID != "" {
		details["resource_ref"] = out.ResourceID
	}
	if out.Status != "" {
		details["status"] = out.Status
	}
	if out.Error != "" {
		details["error"] = capRunError(out.Error)
	}

	origin, err := store.GetByID(ctx, auditID)
	if err != nil && logger != nil {
		logger.Warn("audit: could not read the row that started a run", "audit_id", auditID, "error", err)
	}
	if origin != nil {
		inheritOrigin(entry, details, origin)
	}

	raw, err := json.Marshal(details)
	if err != nil {
		raw = []byte("{}")
	}
	entry.Details = raw
	if err := store.Create(ctx, entry); err != nil && logger != nil {
		logger.Warn("audit: run outcome insert failed", "action", entry.Action, "audit_id", auditID, "error", err)
	}
}

// capRunError keeps at most maxRunErrorLen bytes of a failure message.
func capRunError(msg string) string {
	if len(msg) <= maxRunErrorLen {
		return msg
	}
	return strings.ToValidUTF8(msg[:maxRunErrorLen-3], "") + "..."
}

// inheritOrigin copies who made a change (user, address, user agent) and its
// via, approval_ref, token, and project from the row that started it.
func inheritOrigin(entry *models.AuditLog, details map[string]interface{}, origin *models.AuditLog) {
	details["origin_action"] = origin.Action
	entry.UserID, entry.IPAddress, entry.UserAgent = origin.UserID, origin.IPAddress, origin.UserAgent
	od, err := origin.GetDetails()
	if err != nil {
		return
	}
	for _, k := range []string{"via", "approval_ref", "token_id", "token_name", "project_id"} {
		if v, ok := od[k]; ok {
			details[k] = v
		}
	}
	if _, ok := details["project_id"]; ok {
		return
	}
	if t, ok := od["target"].(map[string]interface{}); ok {
		if pid, ok := t["project_id"]; ok {
			details["project_id"] = pid
		}
	}
}
