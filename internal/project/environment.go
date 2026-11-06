// Package project provides project and environment management services.
package project

import (
	"context"
	"fmt"

	"github.com/raythurman2386/goforge/internal/docker"

	"github.com/raythurman2386/goforge/internal/models"
)

// ListEnvironments retrieves all environments for a project.
func (s *Service) ListEnvironments(ctx context.Context, projectID string) ([]*models.Environment, error) {
	return s.environmentRepo.ListByProjectID(ctx, projectID)
}

// CreateEnvironment creates a new environment for a project.
func (s *Service) CreateEnvironment(ctx context.Context, env *models.Environment) error {
	return s.environmentRepo.Create(ctx, env)
}

// GetEnvironment retrieves an environment by ID.
func (s *Service) GetEnvironment(ctx context.Context, id string) (*models.Environment, error) {
	return s.environmentRepo.GetByID(ctx, id)
}

// UpdateEnvironment updates environment settings.
func (s *Service) UpdateEnvironment(ctx context.Context, env *models.Environment) error {
	return s.environmentRepo.Update(ctx, env)
}

// DeleteEnvironment removes every labeled app container, including a rollback
// copy, before deleting the environment row and its database-owned containers.
func (s *Service) DeleteEnvironment(ctx context.Context, id string) error {
	if err := s.removeLabeledContainers(ctx, docker.EnvironmentLabelKey, id); err != nil {
		return fmt.Errorf("failed to remove environment containers: %w", err)
	}
	return s.environmentRepo.Delete(ctx, id)
}
