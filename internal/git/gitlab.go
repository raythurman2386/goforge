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

// LabProvider implements the Provider interface for GitLab.
type LabProvider struct{}

// NewGitLabProvider creates a new LabProvider.
func NewGitLabProvider() Provider {
	return &LabProvider{}
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

func (p *LabProvider) doRequest(ctx context.Context, source *models.GitSource, method, path string) (*http.Response, error) {
	apiURL := "https://gitlab.com/api/v4"
	if source.APIURL.Valid && source.APIURL.String != "" {
		apiURL = source.APIURL.String
		// Ensure no trailing slash
		if apiURL[len(apiURL)-1] == '/' {
			apiURL = apiURL[:len(apiURL)-1]
		}
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

	req, err := http.NewRequestWithContext(ctx, method, fullURL, http.NoBody)
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
func (p *LabProvider) ListRepositories(ctx context.Context, source *models.GitSource) ([]*Repository, error) {
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
	for i := range glProjects {
		r := &glProjects[i]
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
func (p *LabProvider) ListBranches(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
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
	for i := range glBranches {
		b := &glBranches[i]
		branches = append(branches, &Branch{
			Name: b.Name,
		})
	}

	return branches, nil
}

// ListTags retrieves tags for a repository from GitLab.
func (p *LabProvider) ListTags(ctx context.Context, source *models.GitSource, owner, repo string) ([]*Branch, error) {
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
	for i := range glTags {
		t := &glTags[i]
		tags = append(tags, &Branch{
			Name: t.Name,
		})
	}

	return tags, nil
}

// GetFileContent retrieves the content of a file from GitLab.
func (p *LabProvider) GetFileContent(ctx context.Context, source *models.GitSource, owner, repo, ref, path string) (string, error) {
	// TODO: Implement GitLab file content retrieval
	return "", fmt.Errorf("backend not implemented for GitLab")
}
