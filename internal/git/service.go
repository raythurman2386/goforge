package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/raythurman2386/goforge/internal/models"
)

// Repository represents a git repository.
type Repository struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	URL           string `json:"url"`
	Private       bool   `json:"private"`
}

// Branch represents a git branch.
type Branch struct {
	Name string `json:"name"`
}

// Provider defines the interface for git source providers.
type Provider interface {
	// ListRepositories retrieves repositories from the provider.
	ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error)
	// ListBranches retrieves branches for a repository.
	ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error)
}

// Service handles git-related business logic.
type Service struct {
	providers map[models.GitSourceType]Provider
}

// NewService creates a new git Service.
func NewService() *Service {
	return &Service{
		providers: map[models.GitSourceType]Provider{
			models.GitSourceTypeGitHub: NewGitHubProvider(),
		},
	}
}

// ListRepositories retrieves repositories from the specified source.
func (s *Service) ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error) {
	provider, ok := s.providers[source.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported git source type: %s", source.Type)
	}
	return provider.ListRepositories(ctx, source)
}

// ListBranches retrieves branches for a repository from the specified source.
func (s *Service) ListBranches(ctx context.Context, source *models.GitSource, fullName string) ([]*Branch, error) {
	provider, ok := s.providers[source.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported git source type: %s", source.Type)
	}

	parts := strings.Split(fullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository full name: %s", fullName)
	}

	return provider.ListBranches(ctx, source, parts[0], parts[1])
}
