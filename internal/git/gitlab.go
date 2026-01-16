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

// GitLabProvider implements the Provider interface for GitLab.
type GitLabProvider struct{}

// NewGitLabProvider creates a new GitLabProvider.
func NewGitLabProvider() *GitLabProvider {
	return &GitLabProvider{}
}

type gitlabProject struct {
	ID                int       `json:"id"`
	PathWithNamespace string    `json:"path_with_namespace"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	DefaultBranch     string    `json:"default_branch"`
	WebURL            string    `json:"web_url"`
	Visibility        string    `json:"visibility"`
	LastActivityAt    time.Time `json:"last_activity_at"`
}

type gitlabBranch struct {
	Name string `json:"name"`
}

func (p *GitLabProvider) doRequest(ctx context.Context, source *models.GitSource, method, path string) (*http.Response, error) {
	apiURL := "https://gitlab.com/api/v4"
	if source.APIURL.Valid && source.APIURL.String != "" {
		apiURL = source.APIURL.String
		// Ensure no trailing slash
		if apiURL[len(apiURL)-1] == '/' {
			apiURL = apiURL[:len(apiURL)-1]
		}
		// If user didn't append /api/v4, we might need to (but standard practice is user provides full base API URL or just host)
		// For safety, let's assume user provides base URL like https://gitlab.example.com
		// We should check if it ends with /api/v4
		// Actually, let's just assume the user puts the base URL (e.g. https://gitlab.com) and we append /api/v4
		// But if they provided full path, we respect it?
		// To be safe/simple: Assume input is "https://gitlab.example.com"
		// If it doesn't contain /api/v4, we append it.
		// Wait, commonly people just put the instance URL.
		u, err := url.Parse(apiURL)
		if err == nil {
			if u.Path == "" || u.Path == "/" {
				u.Path = "/api/v4"
				apiURL = u.String()
			}
		}
	}

	fullURL, err := url.JoinPath(apiURL, path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return nil, err
	}

	if source.AccessToken.Valid {
		req.Header.Set("PRIVATE-TOKEN", source.AccessToken.String)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	return client.Do(req)
}

// ListRepositories retrieves repositories from GitLab.
func (p *GitLabProvider) ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error) {
	// membership=true gets projects user is a member of
	resp, err := p.doRequest(ctx, source, "GET", "/projects?membership=true&simple=true&per_page=100&order_by=last_activity_at")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab api error: %s", resp.Status)
	}

	var glProjects []gitlabProject
	if err := json.NewDecoder(resp.Body).Decode(&glProjects); err != nil {
		return nil, err
	}

	repos := make([]*Repository, 0, len(glProjects))
	for _, r := range glProjects {
		repos = append(repos, &Repository{
			Name:          r.Name,
			FullName:      r.PathWithNamespace,
			Description:   r.Description,
			DefaultBranch: r.DefaultBranch,
			URL:           r.WebURL,
			Private:       r.Visibility == "private" || r.Visibility == "internal",
		})
	}

	return repos, nil
}

// ListBranches retrieves branches for a repository from GitLab.
func (p *GitLabProvider) ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
	// Project ID in GitLab API can be "namespace/project_name" URL-encoded
	projectID := url.QueryEscape(fmt.Sprintf("%s/%s", owner, repo))
	path := fmt.Sprintf("/projects/%s/repository/branches", projectID)

	resp, err := p.doRequest(ctx, source, "GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab api error: %s", resp.Status)
	}

	var glBranches []gitlabBranch
	if err := json.NewDecoder(resp.Body).Decode(&glBranches); err != nil {
		return nil, err
	}

	branches := make([]*Branch, 0, len(glBranches))
	for _, b := range glBranches {
		branches = append(branches, &Branch{
			Name: b.Name,
		})
	}

	return branches, nil
}

// ListTags retrieves tags for a repository from GitLab.
func (p *GitLabProvider) ListTags(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
	projectID := url.QueryEscape(fmt.Sprintf("%s/%s", owner, repo))
	path := fmt.Sprintf("/projects/%s/repository/tags", projectID)

	resp, err := p.doRequest(ctx, source, "GET", path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab api error: %s", resp.Status)
	}

	var glTags []gitlabBranch // Tag structure is similar enough for name
	if err := json.NewDecoder(resp.Body).Decode(&glTags); err != nil {
		return nil, err
	}

	tags := make([]*Branch, 0, len(glTags))
	for _, t := range glTags {
		tags = append(tags, &Branch{
			Name: t.Name,
		})
	}

	return tags, nil
}
