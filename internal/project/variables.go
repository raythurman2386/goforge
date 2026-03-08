package project

import (
	"context"

	"github.com/raythurman2386/goforge/internal/models"
)

// ListEnvVariables retrieves all environment variables for an environment.
func (s *Service) ListEnvVariables(ctx context.Context, envID string) ([]*models.EnvVariable, error) {
	return s.envVariableRepo.ListByEnvironmentID(ctx, envID)
}

// AddEnvVariable adds a new environment variable.
// Note: Encryption is handled automatically by the repository if IsSecret is true.
func (s *Service) AddEnvVariable(ctx context.Context, v *models.EnvVariable) error {
	return s.envVariableRepo.Create(ctx, v)
}

// UpdateEnvVariable updates an environment variable.
// Note: Encryption is handled automatically by the repository if IsSecret is true.
func (s *Service) UpdateEnvVariable(ctx context.Context, v *models.EnvVariable) error {
	return s.envVariableRepo.Update(ctx, v)
}

// DeleteEnvVariable deletes an environment variable.
func (s *Service) DeleteEnvVariable(ctx context.Context, id string) error {
	return s.envVariableRepo.Delete(ctx, id)
}

// DeleteEnvVariableScoped deletes an environment variable only if it belongs to the given environment.
func (s *Service) DeleteEnvVariableScoped(ctx context.Context, id, environmentID string) error {
	return s.envVariableRepo.DeleteScoped(ctx, id, environmentID)
}
