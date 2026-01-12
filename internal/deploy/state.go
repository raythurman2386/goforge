package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/user/gocool/internal/models"
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

	return s.UpdateStatus(ctx, deploymentID, to, errorMsg)
}

func isValidTransition(from, to models.DeploymentStatus) bool {
	// Any state can transition to failed or cancelled
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
	default:
		return false
	}
}

// Log logs a message for a deployment.
func (s *Service) Log(ctx context.Context, deploymentID string, level, source, message string) error {
	// For now, we'll just print to stdout until we implement DeploymentLogRepository
	fmt.Printf("[%s] [%s] [%s] %s\n", time.Now().Format(time.RFC3339), level, source, message)
	return nil
}
