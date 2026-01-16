// Package webhook handles incoming webhooks from git providers.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
)

// Webhook errors.
var (
	ErrInvalidSignature = errors.New("invalid webhook signature")
	ErrInvalidPayload   = errors.New("invalid webhook payload")
	ErrUnsupportedEvent = errors.New("unsupported webhook event")
)

// PushEvent represents a push event from a git provider.
type PushEvent struct {
	Ref            string // Full ref (e.g., "refs/heads/main")
	Branch         string // Extracted branch name
	CommitSHA      string
	CommitMessage  string
	CommitAuthor   string
	RepositoryURL  string
	RepositoryName string // owner/repo format
	IsTag          bool
}

// IsBranchPush returns true if this is a branch push (not a tag).
func (e *PushEvent) IsBranchPush() bool {
	return strings.HasPrefix(e.Ref, "refs/heads/")
}

// Handler handles webhook requests from git providers.
type Handler struct {
	deployService   *deploy.Service
	projectRepo     *repositories.ProjectRepository
	environmentRepo *repositories.EnvironmentRepository
	worker          *deploy.Worker
	webhookSecret   string
}

// NewHandler creates a new webhook Handler.
func NewHandler(
	ds *deploy.Service,
	pr *repositories.ProjectRepository,
	er *repositories.EnvironmentRepository,
	w *deploy.Worker,
	secret string,
) *Handler {
	return &Handler{
		deployService:   ds,
		projectRepo:     pr,
		environmentRepo: er,
		worker:          w,
		webhookSecret:   secret,
	}
}

// writeJSONResponse writes a JSON response. Write errors are ignored as they
// typically indicate the client disconnected.
func writeJSONResponse(w http.ResponseWriter, data string) {
	w.Header().Set("Content-Type", "application/json")
	//nolint:errcheck // Ignore write errors - client may have disconnected
	w.Write([]byte(data))
}

// HandleGitHub handles GitHub webhook requests.
func (h *Handler) HandleGitHub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")

	// Handle ping event (used when setting up webhooks)
	if eventType == "ping" {
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "pong"}`)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify signature (skip for ping events)
	signature := r.Header.Get("X-Hub-Signature-256")
	if !h.verifySignature(signature, body, h.webhookSecret) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse event based on type
	switch eventType {
	case "push":
		event, err := h.parseGitHubPushEvent(body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse push event: %v", err), http.StatusBadRequest)
			return
		}

		// Process the push event (trigger deployment if needed)
		h.processPushEvent(r.Context(), event)

		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "accepted"}`)

	default:
		// Acknowledge but don't process other events
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "ignored", "reason": "unsupported event type"}`)
	}
}

// verifySignature verifies the webhook signature (HMAC-SHA256).
// Compatible with GitHub and Gitea.
func (h *Handler) verifySignature(signature string, payload []byte, secret string) bool {
	if signature == "" {
		return false
	}

	// Some providers might prefix with "sha256=", others might not.
	// GitHub: "sha256=..."
	// Gitea: "..." (hex string) - wait, Gitea documentation says it sends the hex string directly in X-Gitea-Signature?
	// Actually, usually Gitea mimics GitHub if configured, but let's be robust.

	sigHex := signature
	if strings.HasPrefix(signature, "sha256=") {
		sigHex = strings.TrimPrefix(signature, "sha256=")
	}

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	// Compute expected signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

// parseGitHubPushEvent parses a GitHub push event payload.
func (h *Handler) parseGitHubPushEvent(payload []byte) (*PushEvent, error) {
	var raw struct {
		Ref        string `json:"ref"`
		After      string `json:"after"`
		HeadCommit struct {
			ID      string `json:"id"`
			Message string `json:"message"`
			Author  struct {
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"author"`
		} `json:"head_commit"`
		Repository struct {
			FullName string `json:"full_name"`
			HTMLURL  string `json:"html_url"`
		} `json:"repository"`
	}

	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}

	event := &PushEvent{
		Ref:            raw.Ref,
		CommitSHA:      raw.After,
		CommitMessage:  raw.HeadCommit.Message,
		CommitAuthor:   raw.HeadCommit.Author.Name,
		RepositoryURL:  raw.Repository.HTMLURL,
		RepositoryName: raw.Repository.FullName,
	}

	// Extract branch name from ref
	if strings.HasPrefix(raw.Ref, "refs/heads/") {
		event.Branch = strings.TrimPrefix(raw.Ref, "refs/heads/")
	} else if strings.HasPrefix(raw.Ref, "refs/tags/") {
		event.IsTag = true
	}

	return event, nil
}

// HandleGitea handles Gitea webhook requests.
func (h *Handler) HandleGitea(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	eventType := r.Header.Get("X-Gitea-Event")
	if eventType == "" {
		// Fallback to GitHub header if Gitea one is missing (some versions)
		eventType = r.Header.Get("X-GitHub-Event")
	}

	// Handle ping event
	if eventType == "ping" {
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "pong"}`)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Verify signature
	// Gitea uses X-Gitea-Signature (HMAC-SHA256 of payload with secret)
	signature := r.Header.Get("X-Gitea-Signature")
	if signature == "" {
		// Fallback to GitHub signature
		signature = r.Header.Get("X-Hub-Signature-256")
	}

	if !h.verifySignature(signature, body, h.webhookSecret) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	switch eventType {
	case "push":
		// Gitea payload is compatible with GitHub for push events
		event, err := h.parseGitHubPushEvent(body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse push event: %v", err), http.StatusBadRequest)
			return
		}

		h.processPushEvent(r.Context(), event)
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "accepted"}`)

	default:
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "ignored"}`)
	}
}

// processPushEvent processes a push event and triggers deployments.
func (h *Handler) processPushEvent(ctx context.Context, event *PushEvent) {
	fmt.Printf("Received push event: repo=%s branch=%s commit=%s\n",
		event.RepositoryName, event.Branch, event.CommitSHA)

	if !event.IsBranchPush() {
		return
	}

	if h.projectRepo == nil {
		return
	}

	// 1. Look up projects by repository URL
	// We might need to normalize the URL (remove .git suffix, handle http/ssh differences)
	// For now, we assume exact match or simple normalization if needed.
	projects, err := h.projectRepo.ListByRepoURL(ctx, event.RepositoryURL)
	if err != nil {
		fmt.Printf("Failed to list projects for repo %s: %v\n", event.RepositoryURL, err)
		return
	}

	if len(projects) == 0 {
		fmt.Printf("No projects found for repo %s\n", event.RepositoryURL)
		return
	}

	// 2. Iterate projects and their environments
	for _, proj := range projects {
		envs, err := h.environmentRepo.ListByProjectID(ctx, proj.ID)
		if err != nil {
			fmt.Printf("Failed to list environments for project %s: %v\n", proj.ID, err)
			continue
		}

		for _, env := range envs {
			if !env.AutoDeploy {
				continue
			}

			// Determine target branch for this environment
			targetBranch := proj.DefaultBranch
			if env.Branch.Valid && env.Branch.String != "" {
				targetBranch = env.Branch.String
			}

			if targetBranch != event.Branch {
				continue
			}

			// 3. Trigger deployment
			fmt.Printf("Triggering deployment for project %s env %s\n", proj.Name, env.Name)

			d := &models.Deployment{
				EnvironmentID: env.ID,
				Status:        models.DeploymentStatusPending,
				TriggerType:   models.TriggerTypeWebhook,
				CommitSHA:     sql.NullString{String: event.CommitSHA, Valid: true},
				CommitMessage: sql.NullString{String: event.CommitMessage, Valid: true},
				CommitAuthor:  sql.NullString{String: event.CommitAuthor, Valid: true},
				Branch:        sql.NullString{String: event.Branch, Valid: true},
			}

			if err := h.deployService.CreateDeployment(ctx, d); err != nil {
				fmt.Printf("Failed to create deployment record: %v\n", err)
				continue
			}

			// Enqueue for background processing
			h.worker.Enqueue(d.ID)
		}
	}
}

// GitLabPushEvent represents a push event from GitLab.
type GitLabPushEvent struct {
	Ref         string `json:"ref"`
	CheckoutSHA string `json:"checkout_sha"`
	UserName    string `json:"user_name"`
	ProjectID   int    `json:"project_id"`
	ProjectPath string `json:"path_with_namespace"`
	Project     struct {
		WebURL string `json:"web_url"`
	} `json:"project"`
}

// HandleGitLab handles GitLab webhook requests.
func (h *Handler) HandleGitLab(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// GitLab sends a secret token in the header
	token := r.Header.Get("X-Gitlab-Token")
	if token != h.webhookSecret {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-Gitlab-Event")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	switch eventType {
	case "Push Hook":
		event, err := h.parseGitLabPushEvent(body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse push event: %v", err), http.StatusBadRequest)
			return
		}

		pushEvent := &PushEvent{
			Ref:            event.Ref,
			CommitSHA:      event.CheckoutSHA,
			RepositoryURL:  event.Project.WebURL,
			RepositoryName: event.ProjectPath,
		}
		if strings.HasPrefix(event.Ref, "refs/heads/") {
			pushEvent.Branch = strings.TrimPrefix(event.Ref, "refs/heads/")
		}

		h.processPushEvent(r.Context(), pushEvent)

		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "accepted"}`)

	default:
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "ignored"}`)
	}
}

// parseGitLabPushEvent parses a GitLab push event payload.
func (h *Handler) parseGitLabPushEvent(payload []byte) (*GitLabPushEvent, error) {
	var event GitLabPushEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPayload, err)
	}
	return &event, nil
}
