package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/raythurman2386/gocool/internal/models"
)

// Transition handles deployment state transitions.
func (s *Service) Transition(ctx context.Context, deploymentID string, to models.DeploymentStatus, errorMsg *string) error {
	d, err := s.GetDeployment(ctx, deploymentID)
	if err != nil {
		return err
	}

	// Validate transition
	if !isValidTransition(d.Status, to) {
		return fmt.Errorf("invalid state transition from %s to %s", d.Status, to)
	}

	if err := s.UpdateStatus(ctx, deploymentID, to, errorMsg); err != nil {
		return err
	}

	// Broadcast status update
	s.sseHub.Broadcast(fmt.Sprintf("deployment:%s", deploymentID), map[string]interface{}{
		"status": to,
		"error":  errorMsg,
	})

	return nil
}

func isValidTransition(from, to models.DeploymentStatus) bool {
	// Any state can transition to failed or canceled
	if to == models.DeploymentStatusFailed || to == models.DeploymentStatusCanceled {
		return true
	}

	switch from {
	case models.DeploymentStatusPending:
		return to == models.DeploymentStatusCloning
	case models.DeploymentStatusCloning:
		return to == models.DeploymentStatusBuilding
	case models.DeploymentStatusBuilding:
		return to == models.DeploymentStatusDeploying
	case models.DeploymentStatusDeploying:
		return to == models.DeploymentStatusRunning
	case models.DeploymentStatusRunning:
		return to == models.DeploymentStatusRolledBack
	case models.DeploymentStatusFailed, models.DeploymentStatusRolledBack, models.DeploymentStatusCanceled:
		return false
	default:
		return false
	}
}

// Log logs a message for a deployment.
func (s *Service) Log(ctx context.Context, deploymentID, level, source, message string) error {
	// For now, we'll just print to stdout until we implement DeploymentLogRepository
	fmt.Printf("[%s] [%s] [%s] %s\n", time.Now().Format(time.RFC3339), level, source, message)
	return nil
}
