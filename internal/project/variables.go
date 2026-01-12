package project

import (
	"context"

	"github.com/user/gocool/internal/models"
)

// ListEnvVariables retrieves all environment variables for an environment.
func (s *Service) ListEnvVariables(ctx context.Context, envID string) ([]*models.EnvVariable, error) {
	return s.envVariableRepo.ListByEnvironmentID(ctx, envID)
}

// AddEnvVariable adds a new environment variable.
func (s *Service) AddEnvVariable(ctx context.Context, v *models.EnvVariable) error {
	// TODO: Encrypt value if v.IsSecret
	return s.envVariableRepo.Create(ctx, v)
}

// UpdateEnvVariable updates an environment variable.
func (s *Service) UpdateEnvVariable(ctx context.Context, v *models.EnvVariable) error {
	// TODO: Encrypt value if v.IsSecret
	return s.envVariableRepo.Update(ctx, v)
}

// DeleteEnvVariable deletes an environment variable.
func (s *Service) DeleteEnvVariable(ctx context.Context, id string) error {
	return s.envVariableRepo.Delete(ctx, id)
}
