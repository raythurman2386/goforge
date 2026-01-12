package git

import (
	"context"

	"github.com/google/go-github/v53/github"
	"golang.org/x/oauth2"

	"github.com/user/gocool/internal/models"
)

// GitHubProvider implements the Provider interface for GitHub.
type GitHubProvider struct{}

// NewGitHubProvider creates a new GitHubProvider.
func NewGitHubProvider() *GitHubProvider {
	return &GitHubProvider{}
}

func (p *GitHubProvider) getClient(ctx context.Context, source *models.GitSource) *github.Client {
	if !source.AccessToken.Valid {
		return github.NewClient(nil)
	}

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: source.AccessToken.String},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

// ListRepositories retrieves repositories from GitHub.
func (p *GitHubProvider) ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error) {
	client := p.getClient(ctx, source)

	// List repositories for the authenticated user
	opts := &github.RepositoryListOptions{
		Sort: "updated",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	ghRepos, _, err := client.Repositories.List(ctx, "", opts)
	if err != nil {
		return nil, err
	}

	var repos []*Repository
	for _, r := range ghRepos {
		repos = append(repos, &Repository{
			Name:          r.GetName(),
			FullName:      r.GetFullName(),
			Description:   r.GetDescription(),
			DefaultBranch: r.GetDefaultBranch(),
			URL:           r.GetHTMLURL(),
			Private:       r.GetPrivate(),
		})
	}

	return repos, nil
}

// ListBranches retrieves branches for a repository from GitHub.
func (p *GitHubProvider) ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
	client := p.getClient(ctx, source)

	opts := &github.BranchListOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	ghBranches, _, err := client.Repositories.ListBranches(ctx, owner, repo, opts)
	if err != nil {
		return nil, err
	}

	var branches []*Branch
	for _, b := range ghBranches {
		branches = append(branches, &Branch{
			Name: b.GetName(),
		})
	}

	return branches, nil
}
