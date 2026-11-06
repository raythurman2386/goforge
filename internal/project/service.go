package project

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	mobycontainer "github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
)

// Service handles project and environment business logic.
type Service struct {
	projectRepo     projectStore
	environmentRepo environmentStore
	envVariableRepo *repositories.EnvVariableRepository
	deploymentRepo  *repositories.DeploymentRepository
	containerRepo   containerStore
	gitSourceRepo   *repositories.GitSourceRepository
	gitService      *git.Service
	dockerClient    containerRuntime
	audit           CleanupAuditStore
}

// projectStore is the repository behavior the project service needs.
type projectStore interface {
	Create(ctx context.Context, p *models.Project) error
	GetByID(ctx context.Context, id string) (*models.Project, error)
	Delete(ctx context.Context, id string) error
	Update(ctx context.Context, p *models.Project) error
	ListByUserID(ctx context.Context, userID string) ([]*models.Project, error)
}

// environmentStore is the repository behavior needed for parent lookup and deletion.
type environmentStore interface {
	Create(ctx context.Context, env *models.Environment) error
	GetByID(ctx context.Context, id string) (*models.Environment, error)
	Update(ctx context.Context, env *models.Environment) error
	Delete(ctx context.Context, id string) error
	ListByProjectID(ctx context.Context, projectID string) ([]*models.Environment, error)
}

// containerStore removes stale host container records.
type containerStore interface {
	DeleteByDockerID(ctx context.Context, dockerID string) error
}

// containerRuntime lists and removes app containers; tests use a fake.
type containerRuntime interface {
	ListContainers(ctx context.Context, all bool) ([]mobycontainer.Summary, error)
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
}

// CleanupAuditStore persists orphan removal records.
type CleanupAuditStore interface {
	Create(ctx context.Context, l *models.AuditLog) error
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

	// Try to auto-populate environment variables from .env.example if git source is available
	if p.GitSourceID.Valid && s.gitService != nil && s.gitSourceRepo != nil {
		source, err := s.gitSourceRepo.GetByID(ctx, p.GitSourceID.String)
		if err == nil {
			s.seedEnvVarsFromRepo(ctx, env.ID, p.RepositoryURL, p.DefaultBranch, source)
		}
	}

	return p, nil
}

// SeedEnvVarsFromRepo fetches .env.example from the repo and seeds env vars into the given
// environment. Only keys that do not already exist are inserted (idempotent).
// Returns the number of new variables inserted and any non-fatal diagnostic error.
func (s *Service) SeedEnvVarsFromRepo(ctx context.Context, envID, projectID string) (int, error) {
	if s.gitService == nil || s.gitSourceRepo == nil {
		return 0, errors.New("git service not configured")
	}

	p, err := s.projectRepo.GetByID(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("project not found: %w", err)
	}
	if !p.GitSourceID.Valid {
		return 0, errors.New("project has no git source configured")
	}

	source, err := s.gitSourceRepo.GetByID(ctx, p.GitSourceID.String)
	if err != nil {
		return 0, fmt.Errorf("git source not found: %w", err)
	}

	// Fetch existing keys so we don't overwrite
	existing, err := s.envVariableRepo.ListByEnvironmentID(ctx, envID)
	if err != nil {
		return 0, fmt.Errorf("failed to list existing variables: %w", err)
	}
	existingKeys := make(map[string]bool, len(existing))
	for _, v := range existing {
		existingKeys[v.Key] = true
	}

	vars, fetchErr := s.fetchEnvExample(ctx, p.RepositoryURL, p.DefaultBranch, source)
	if fetchErr != nil {
		return 0, fetchErr
	}

	inserted := 0
	for _, v := range vars {
		if existingKeys[v.Key] {
			continue
		}
		v.EnvironmentID = envID
		if createErr := s.envVariableRepo.Create(ctx, v); createErr != nil {
			continue // best effort
		}
		inserted++
	}
	return inserted, nil
}

// seedEnvVarsFromRepo is the internal helper used during CreateProject.
// It is idempotent: keys that already exist in the environment are skipped.
func (s *Service) seedEnvVarsFromRepo(ctx context.Context, envID, repoURL, branch string, source *models.GitSource) {
	vars, err := s.fetchEnvExample(ctx, repoURL, branch, source)
	if err != nil {
		return // best effort — don't fail project creation
	}

	existing, err := s.envVariableRepo.ListByEnvironmentID(ctx, envID)
	if err != nil {
		return
	}
	existingKeys := make(map[string]bool, len(existing))
	for _, v := range existing {
		existingKeys[v.Key] = true
	}

	for _, v := range vars {
		if existingKeys[v.Key] {
			continue
		}
		v.EnvironmentID = envID
		//nolint:errcheck // best effort seeding during project creation
		_ = s.envVariableRepo.Create(ctx, v)
	}
}

// fetchEnvExample retrieves .env.example from the repository and parses it into EnvVariable records.
func (s *Service) fetchEnvExample(ctx context.Context, repoURL, branch string, source *models.GitSource) ([]*models.EnvVariable, error) {
	fullName := repoFullName(repoURL)
	if fullName == "" {
		return nil, fmt.Errorf("could not determine repository name from URL: %s", repoURL)
	}

	content, err := s.gitService.GetFileContent(ctx, source, fullName, branch, ".env.example")
	if err != nil || content == "" {
		return nil, fmt.Errorf("could not fetch .env.example: %w", err)
	}

	return parseEnvFile(content), nil
}

// repoFullName extracts "owner/repo" from a repository URL.
func repoFullName(repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	return strings.TrimSuffix(p, ".git")
}

// secretKeyWords lists individual word segments that indicate a key holds a sensitive value.
// Matching is performed against underscore-delimited segments of the key name (case-insensitive),
// so "KEYWORD" does NOT match "key" but "API_KEY" does.
var secretKeyWords = []string{
	"password", "passwd", "secret", "token", "key", "private",
	"cert", "auth", "credential", "apikey", "signing", "encryption", "jwt", "oauth",
}

// secretKeyPairs lists adjacent two-segment patterns (e.g. "access"+"key") that together
// indicate a sensitive value. Each entry is [segment0, segment1].
var secretKeyPairs = [][2]string{
	{"access", "key"},
	{"api", "key"},
}

// isSecretKey returns true if the key name suggests it holds a sensitive value.
// It splits the key on underscores and matches against whole word segments to avoid
// false positives like KEYWORD → "key" or AUTHENTICATION → "auth".
func isSecretKey(key string) bool {
	segments := strings.Split(strings.ToLower(key), "_")
	segSet := make(map[string]bool, len(segments))
	for _, s := range segments {
		segSet[s] = true
	}

	// Single-segment match
	for _, w := range secretKeyWords {
		if segSet[w] {
			return true
		}
	}

	// Adjacent two-segment match (e.g. api_key, access_key)
	for _, pair := range secretKeyPairs {
		for i := 0; i < len(segments)-1; i++ {
			if segments[i] == pair[0] && segments[i+1] == pair[1] {
				return true
			}
		}
	}

	return false
}

// parseEnvFile parses .env-style file content into EnvVariable records.
// Non-secret values (e.g. PORT=3000, NODE_ENV=production) are preserved.
// Keys that match secret patterns are imported with an empty value and IsSecret=true.
func parseEnvFile(content string) []*models.EnvVariable {
	lines := strings.Split(content, "\n")
	result := make([]*models.EnvVariable, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		if key == "" {
			continue
		}

		value := ""
		if len(parts) == 2 {
			// Strip inline comments and surrounding quotes from the value
			value = strings.TrimSpace(parts[1])
			value = stripInlineComment(value)
			value = stripQuotes(value)
		}

		secret := isSecretKey(key)
		if secret {
			value = "" // never import secret values from example files
		}

		result = append(result, &models.EnvVariable{
			Key:      key,
			Value:    value,
			IsSecret: secret,
		})
	}
	return result
}

// stripInlineComment removes a trailing # comment from a value (only when outside quotes).
func stripInlineComment(s string) string {
	// Only strip if not inside a quoted string
	if s != "" && (s[0] == '"' || s[0] == '\'') {
		return s
	}
	if idx := strings.Index(s, " #"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

// stripQuotes removes surrounding single or double quotes from a value.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
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

// WithAudit enables audit rows for background orphan cleanup.
func (s *Service) WithAudit(store CleanupAuditStore) *Service {
	s.audit = store
	return s
}

// DeleteProject deletes a project only after every labeled app container,
// including rollback-restored copies, has been removed from Docker.
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	if err := s.removeLabeledContainers(ctx, docker.ProjectLabelKey, id); err != nil {
		return fmt.Errorf("failed to remove project containers: %w", err)
	}
	return s.projectRepo.Delete(ctx, id)
}

func (s *Service) removeLabeledContainers(ctx context.Context, label, id string) error {
	if !runtimeAvailable(s.dockerClient) {
		return nil
	}
	containers, err := s.dockerClient.ListContainers(ctx, true)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	var errs []error
	for i := range containers {
		c := &containers[i]
		if c.Labels[docker.ManagedLabelKey] != docker.ManagedLabelValue || c.Labels[label] != id {
			continue
		}
		if err := s.removeContainer(ctx, c.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) removeContainer(ctx context.Context, id string) error {
	if err := s.dockerClient.StopContainer(ctx, id); err != nil && !docker.IsNotFound(err) {
		return fmt.Errorf("stop container %s: %w", id, err)
	}
	if err := s.dockerClient.RemoveContainer(ctx, id); err != nil && !docker.IsNotFound(err) {
		return fmt.Errorf("remove container %s: %w", id, err)
	}
	if s.containerRepo != nil {
		if err := s.containerRepo.DeleteByDockerID(ctx, id); err != nil && !errors.Is(err, repositories.ErrNotFound) {
			return fmt.Errorf("delete container row %s: %w", id, err)
		}
	}
	return nil
}

func runtimeAvailable(runtime containerRuntime) bool {
	if runtime == nil {
		return false
	}
	value := reflect.ValueOf(runtime)
	return value.Kind() != reflect.Ptr || !value.IsNil()
}
