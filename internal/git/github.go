// Package git provides git operations and provider integrations.
package git

import (
	"context"

	"github.com/google/go-github/v53/github"
	"golang.org/x/oauth2"

	"github.com/raythurman2386/goforge/internal/models"
)

// HubProvider implements the Provider interface for GitHub.
type HubProvider struct{}

// NewGitHubProvider creates a new HubProvider (previously GitHubProvider).
func NewGitHubProvider() *HubProvider {
	return &HubProvider{}
}

func (p *HubProvider) getClient(ctx context.Context, source *models.GitSource) *github.Client {
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
func (p *HubProvider) ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error) {
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

	repos := make([]*Repository, 0, len(ghRepos))
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
func (p *HubProvider) ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
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

	branches := make([]*Branch, 0, len(ghBranches))
	for _, b := range ghBranches {
		branches = append(branches, &Branch{
			Name: b.GetName(),
		})
	}

	return branches, nil
}

// ListTags retrieves tags for a repository from GitHub.
func (p *HubProvider) ListTags(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
	client := p.getClient(ctx, source)

	opts := &github.ListOptions{
		PerPage: 100,
	}

	ghTags, _, err := client.Repositories.ListTags(ctx, owner, repo, opts)
	if err != nil {
		return nil, err
	}

	tags := make([]*Branch, 0, len(ghTags))
	for _, t := range ghTags {
		tags = append(tags, &Branch{
			Name: t.GetName(),
		})
	}

	return tags, nil
}
