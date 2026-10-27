package deploy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/raythurman2386/goforge/internal/models"
)

// ErrCanceled means a deployment was canceled and must stay canceled.
var ErrCanceled = errors.New("deployment canceled")

// Transition handles deployment state transitions.
// The lock keeps a cancel from being overwritten by a pipeline transition
// that already read the previous status.
func (s *Service) Transition(ctx context.Context, deploymentID string, to models.DeploymentStatus, errorMsg *string) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	d, err := s.GetDeployment(ctx, deploymentID)
	if err != nil {
		return err
	}

	if !isValidTransition(d.Status, to) {
		return fmt.Errorf("invalid state transition from %s to %s", d.Status, to)
	}

	if err := s.UpdateStatus(ctx, deploymentID, to, errorMsg); err != nil {
		return err
	}

	if s.sseHub != nil {
		s.sseHub.Broadcast(fmt.Sprintf("deployment:%s", deploymentID), map[string]interface{}{
			"kind":   "status",
			"status": to,
			"error":  errorMsg,
		})
	}

	return nil
}

func isValidTransition(from, to models.DeploymentStatus) bool {
	failedOrCanceled := to == models.DeploymentStatusFailed || to == models.DeploymentStatusCanceled
	switch from {
	case models.DeploymentStatusPending:
		return to == models.DeploymentStatusCloning || failedOrCanceled
	case models.DeploymentStatusCloning:
		return to == models.DeploymentStatusBuilding || failedOrCanceled
	case models.DeploymentStatusBuilding:
		return to == models.DeploymentStatusDeploying || failedOrCanceled
	case models.DeploymentStatusDeploying:
		return to == models.DeploymentStatusRunning || failedOrCanceled
	case models.DeploymentStatusRunning:
		return to == models.DeploymentStatusRolledBack || failedOrCanceled
	case models.DeploymentStatusFailed, models.DeploymentStatusRolledBack, models.DeploymentStatusCanceled:
		return false
	default:
		return false
	}
}

// Log stores a deployment log line and broadcasts it on the deployment SSE topic.
func (s *Service) Log(ctx context.Context, deploymentID, level, source, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}

	entry := &models.DeploymentLog{
		DeploymentID: deploymentID,
		Level:        normalizeLogLevel(level),
		Message:      message,
		Source:       sql.NullString{String: source, Valid: source != ""},
	}
	if s.logRepo != nil {
		if err := s.logRepo.Create(ctx, entry); err != nil {
			if s.logger != nil {
				s.logger.Error("failed to store deployment log", "deployment_id", deploymentID, "error", err)
			}
			return fmt.Errorf("failed to store deployment log: %w", err)
		}
	}
	if s.logger != nil {
		s.logger.Log(ctx, slogLevel(level), message, "deployment_id", deploymentID, "source", source)
	}
	if s.sseHub != nil {
		s.sseHub.Broadcast(fmt.Sprintf("deployment:%s", deploymentID), map[string]interface{}{
			"kind":      "log",
			"id":        entry.ID,
			"level":     string(entry.Level),
			"source":    source,
			"message":   message,
			"timestamp": entry.Timestamp,
		})
	}
	return nil
}

func normalizeLogLevel(level string) models.DeploymentLogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return models.LogLevelDebug
	case "warn", "warning":
		return models.LogLevelWarn
	case "error":
		return models.LogLevelError
	default:
		return models.LogLevelInfo
	}
}

// slogLevel converts a string log level to slog.Level.
func slogLevel(level string) slog.Level {
	switch level {
	case "error", "ERROR":
		return slog.LevelError
	case "warn", "WARN", "warning", "WARNING":
		return slog.LevelWarn
	case "debug", "DEBUG":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}
