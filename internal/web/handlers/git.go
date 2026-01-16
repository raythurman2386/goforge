package handlers

import (
	"net/http"
	"strings"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/git"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
)

// GitHandler handles git related requests.
type GitHandler struct {
	gitSourceRepo *repositories.GitSourceRepository
	gitService    *git.Service
}

// NewGitHandler creates a new GitHandler.
func NewGitHandler(gsr *repositories.GitSourceRepository, gs *git.Service) *GitHandler {
	return &GitHandler{
		gitSourceRepo: gsr,
		gitService:    gs,
	}
}

// ListRepositories lists repositories for a git source.
func (h *GitHandler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	sourceID := r.URL.Query().Get("source_id")
	if sourceID == "" {
		// Return empty or manual input indicator if needed,
		// but here we likely just want to return nothing or handle it gracefully
		return
	}

	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	source, err := h.gitSourceRepo.GetByID(r.Context(), sourceID)
	if err != nil {
		http.Error(w, "Git source not found", http.StatusNotFound)
		return
	}

	if source.UserID != user.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	repos, err := h.gitService.ListRepositories(r.Context(), source)
	if err != nil {
		http.Error(w, "Failed to list repositories: "+err.Error(), http.StatusInternalServerError)
		return
	}

	component := components.RepoOptions(repos)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "Failed to render", http.StatusInternalServerError)
	}
}

// ListBranches lists branches for a repository.
func (h *GitHandler) ListBranches(w http.ResponseWriter, r *http.Request) {
	sourceID := r.URL.Query().Get("source_id")
	repoURL := r.URL.Query().Get("repository_url")

	if sourceID == "" || repoURL == "" {
		return
	}

	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	source, err := h.gitSourceRepo.GetByID(r.Context(), sourceID)
	if err != nil {
		http.Error(w, "Git source not found", http.StatusNotFound)
		return
	}

	if source.UserID != user.ID {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	owner, repo := parseOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		http.Error(w, "Invalid repository URL", http.StatusBadRequest)
		return
	}
	fullName := owner + "/" + repo

	branches, err := h.gitService.ListBranches(r.Context(), source, fullName)
	if err != nil {
		http.Error(w, "Failed to list branches: "+err.Error(), http.StatusInternalServerError)
		return
	}

	component := components.BranchOptions(branches, "main")
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "Failed to render", http.StatusInternalServerError)
	}
}

// ListTags lists tags for a repository.
func (h *GitHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	// Logic similar to ListBranches, implemented if UI switches to tags
	// For now, let's assume branches are primary.
	// If needed, we can add a toggle in UI to call this.
}

func parseOwnerRepo(repoURL string) (owner, repo string) {
	// Simple heuristic for common git URLs
	// https://github.com/owner/repo.git
	// git@github.com:owner/repo.git
	repoURL = strings.TrimSuffix(repoURL, ".git")
	repoURL = strings.TrimSuffix(repoURL, "/")

	if strings.HasPrefix(repoURL, "http") {
		parts := strings.Split(repoURL, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	} else if strings.Contains(repoURL, ":") {
		// SSH format
		parts := strings.Split(repoURL, ":")
		if len(parts) == 2 {
			parts = strings.Split(parts[1], "/")
			if len(parts) == 2 {
				return parts[0], parts[1]
			}
		}
	}

	return "", ""
}
