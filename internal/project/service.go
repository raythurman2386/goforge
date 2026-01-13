package project

import (
	"context"
	"fmt"

	"github.com/raythurman2386/gocool/internal/database/repositories"
	"github.com/raythurman2386/gocool/internal/models"
)

// Service handles project and environment business logic.
type Service struct {
	projectRepo     *repositories.ProjectRepository
	environmentRepo *repositories.EnvironmentRepository
	envVariableRepo *repositories.EnvVariableRepository
}

// NewService creates a new project Service.
func NewService(pr *repositories.ProjectRepository, er *repositories.EnvironmentRepository, evr *repositories.EnvVariableRepository) *Service {
	return &Service{
		projectRepo:     pr,
		environmentRepo: er,
		envVariableRepo: evr,
	}
}

// CreateProject creates a new project and a default 'production' environment.
func (s *Service) CreateProject(ctx context.Context, p *models.Project) (*models.Project, error) {
	if err := s.projectRepo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("failed to create project: %w", err)
	}

	// Create default production environment
	env := &models.Environment{
		ProjectID: p.ID,
		Name:      "production",
	}
	if err := s.environmentRepo.Create(ctx, env); err != nil {
		return nil, fmt.Errorf("failed to create default environment: %w", err)
	}

	return p, nil
}

// GetProject retrieves a project by ID.
func (s *Service) GetProject(ctx context.Context, id string) (*models.Project, error) {
	return s.projectRepo.GetByID(ctx, id)
}

// ListProjects retrieves all projects for a user.
func (s *Service) ListProjects(ctx context.Context, userID string) ([]*models.Project, error) {
	return s.projectRepo.ListByUserID(ctx, userID)
}

// UpdateProject updates project settings.
func (s *Service) UpdateProject(ctx context.Context, p *models.Project) error {
	return s.projectRepo.Update(ctx, p)
}

// DeleteProject deletes a project and all associated resources.
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	return s.projectRepo.Delete(ctx, id)
}
