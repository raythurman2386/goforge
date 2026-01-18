package project

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
)

// Service handles project and environment business logic.
type Service struct {
	projectRepo     *repositories.ProjectRepository
	environmentRepo *repositories.EnvironmentRepository
	envVariableRepo *repositories.EnvVariableRepository
	deploymentRepo  *repositories.DeploymentRepository
	containerRepo   *repositories.ContainerRepository
	gitSourceRepo   *repositories.GitSourceRepository
	gitService      *git.Service
	dockerClient    *docker.Client
}

// NewService creates a new project Service.
func NewService(
	pr *repositories.ProjectRepository,
	er *repositories.EnvironmentRepository,
	evr *repositories.EnvVariableRepository,
	dr *repositories.DeploymentRepository,
	cr *repositories.ContainerRepository,
	gsr *repositories.GitSourceRepository,
	gs *git.Service,
	dc *docker.Client,
) *Service {
	return &Service{
		projectRepo:     pr,
		environmentRepo: er,
		envVariableRepo: evr,
		deploymentRepo:  dr,
		containerRepo:   cr,
		gitSourceRepo:   gsr,
		gitService:      gs,
		dockerClient:    dc,
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

	// Try to auto-populate environment variables from env.example if git source is available
	if p.GitSourceID.Valid && s.gitService != nil && s.gitSourceRepo != nil {
		source, err := s.gitSourceRepo.GetByID(ctx, p.GitSourceID.String)
		if err == nil {
			// Extract owner/repo from URL
			fullName := ""
			u, err := url.Parse(p.RepositoryURL)
			if err == nil {
				path := strings.Trim(u.Path, "/")
				fullName = path // Many providers use path as fullName (owner/repo)
				// Clean up .git suffix if present
				fullName = strings.TrimSuffix(fullName, ".git")
			}

			if fullName != "" {
				content, err := s.gitService.GetFileContent(ctx, source, fullName, p.DefaultBranch, "env.example")
				if err == nil && content != "" {
					envVars := parseEnvFile(content)
					for key := range envVars {
						if err := s.envVariableRepo.Create(ctx, &models.EnvVariable{
							EnvironmentID: env.ID,
							Key:           key,
							Value:         "",
						}); err != nil {
							// Best effort: ignore error if one variable fails
							continue
						}
					}
				}
			}
		}
	}

	return p, nil
}

// parseEnvFile parses a .env style file content and returns a map of keys.
// It ignores comments and empty lines. Values are ignored (keys set to empty string).
func parseEnvFile(content string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle "KEY=VALUE"
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		if key != "" {
			result[key] = "" // Default to empty value for security/safety
		}
	}
	return result
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

// DeleteProject deletes a project and all associated resources (containers, deployments, etc.).
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	// 1. List Environments
	envs, err := s.environmentRepo.ListByProjectID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to list environments for cleanup: %w", err)
	}

	for _, env := range envs {
		// 2. List Deployments
		deployments, err := s.deploymentRepo.ListByEnvironmentID(ctx, env.ID, 1000) // Fetch reasonably all
		if err != nil {
			return fmt.Errorf("failed to list deployments for cleanup: %w", err)
		}

		for _, d := range deployments {
			// 3. List Containers
			containers, err := s.containerRepo.ListByDeploymentID(ctx, d.ID)
			if err != nil {
				return fmt.Errorf("failed to list containers for cleanup: %w", err)
			}

			// 4. Stop and Remove Containers
			for _, c := range containers {
				if c.DockerID != "" && s.dockerClient != nil {
					// Ignore errors here to ensure we try to clean everything
					//nolint:errcheck // We want to proceed even if cleanup fails
					s.dockerClient.StopContainer(ctx, c.DockerID)
					//nolint:errcheck // We want to proceed even if cleanup fails
					s.dockerClient.RemoveContainer(ctx, c.DockerID)
				}
			}
		}
	}

	// 5. Delete Project Record (cascade should handle database relations)
	return s.projectRepo.Delete(ctx, id)
}
