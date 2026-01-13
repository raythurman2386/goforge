// Package project provides project and environment management services.
package project

import (
	"context"

	"github.com/raythurman2386/gocool/internal/models"
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

// DeleteEnvironment deletes an environment.
func (s *Service) DeleteEnvironment(ctx context.Context, id string) error {
	return s.environmentRepo.Delete(ctx, id)
}
