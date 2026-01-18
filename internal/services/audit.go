package services

import (
	"context"
	"encoding/json"
	"log/slog"
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

// Record records a new audit log entry.
func (s *AuditService) Record(ctx context.Context, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string) {
	// Execute in background to not block the request
	//nolint:contextcheck // background task needs its own context
	go func() {
		// Create a new context with timeout for the background operation
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		logEntry := &models.AuditLog{
			ID:           uuid.New().String(),
			Action:       action,
			ResourceType: resourceType,
			CreatedAt:    time.Now(),
		}

		if userID != "" {
			logEntry.UserID.String = userID
			logEntry.UserID.Valid = true
		}
		if resourceID != "" {
			logEntry.ResourceID.String = resourceID
			logEntry.ResourceID.Valid = true
		}
		if ip != "" {
			logEntry.IPAddress.String = ip
			logEntry.IPAddress.Valid = true
		}
		if userAgent != "" {
			logEntry.UserAgent.String = userAgent
			logEntry.UserAgent.Valid = true
		}

		if details != nil {
			jsonBytes, err := json.Marshal(details)
			if err != nil {
				s.logger.Error("failed to marshal audit details", "error", err)
				return
			}
			logEntry.Details = jsonBytes
		}

		if err := s.repo.Create(bgCtx, logEntry); err != nil {
			s.logger.Error("failed to create audit log", "error", err)
		}
	}()
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
