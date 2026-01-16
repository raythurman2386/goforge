package git

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// TeaProvider implements the Provider interface for Gitea.
type TeaProvider struct{}

// NewGiteaProvider creates a new TeaProvider.
func NewGiteaProvider() Provider {
	return &TeaProvider{}
}

type giteaRepository struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Private       bool   `json:"private"`
}

type giteaBranch struct {
	Name string `json:"name"`
}

func (p *TeaProvider) doRequest(ctx context.Context, source *models.GitSource, method, path string) (*http.Response, error) {
	apiURL := "https://gitea.com/api/v1" // Default, though rarely used without custom URL
	if source.APIURL.Valid && source.APIURL.String != "" {
		apiURL = source.APIURL.String
		// Gitea API usually at /api/v1
		u, err := url.Parse(apiURL)
		if err == nil {
			if u.Path == "" || u.Path == "/" {
				u.Path = "/api/v1"
				apiURL = u.String()
			}
		}
	}

	fullURL, err := url.JoinPath(apiURL, path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	if source.AccessToken.Valid {
		req.Header.Set("Authorization", "token "+source.AccessToken.String)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

// ListRepositories retrieves repositories from Gitea.
func (p *TeaProvider) ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error) {
	resp, err := p.doRequest(ctx, source, "GET", "/user/repos?limit=100&sort=updated")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea api error: %s", resp.Status)
	}

	var giteaRepos []giteaRepository
	if err := json.NewDecoder(resp.Body).Decode(&giteaRepos); err != nil {
		return nil, err
	}

	repos := make([]*Repository, 0, len(giteaRepos))
	for _, r := range giteaRepos {
		repos = append(repos, &Repository{
			Name:          r.Name,
			FullName:      r.FullName,
			Description:   r.Description,
			DefaultBranch: r.DefaultBranch,
			URL:           r.HTMLURL,
			Private:       r.Private,
		})
	}

	return repos, nil
}

// ListBranches retrieves branches for a repository from Gitea.
func (p *TeaProvider) ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
	path := fmt.Sprintf("/repos/%s/%s/branches", owner, repo)
	resp, err := p.doRequest(ctx, source, "GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea api error: %s", resp.Status)
	}

	var giteaBranches []giteaBranch
	if err := json.NewDecoder(resp.Body).Decode(&giteaBranches); err != nil {
		return nil, err
	}

	branches := make([]*Branch, 0, len(giteaBranches))
	for _, b := range giteaBranches {
		branches = append(branches, &Branch{
			Name: b.Name,
		})
	}

	return branches, nil
}

// ListTags retrieves tags for a repository from Gitea.
func (p *TeaProvider) ListTags(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
	path := fmt.Sprintf("/repos/%s/%s/tags", owner, repo)
	resp, err := p.doRequest(ctx, source, "GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea api error: %s", resp.Status)
	}

	var giteaTags []giteaBranch // Tag structure is similar enough
	if err := json.NewDecoder(resp.Body).Decode(&giteaTags); err != nil {
		return nil, err
	}

	tags := make([]*Branch, 0, len(giteaTags))
	for _, t := range giteaTags {
		tags = append(tags, &Branch{
			Name: t.Name,
		})
	}

	return tags, nil
}
