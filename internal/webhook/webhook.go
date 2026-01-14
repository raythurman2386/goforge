// Package webhook handles incoming webhooks from git providers.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/raythurman2386/gocool/internal/deploy"
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
	deployService *deploy.Service
	webhookSecret string
}

// NewHandler creates a new webhook Handler.
func NewHandler(ds *deploy.Service, secret string) *Handler {
	return &Handler{
		deployService: ds,
		webhookSecret: secret,
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
	if !h.verifyGitHubSignature(signature, body, h.webhookSecret) {
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

// verifyGitHubSignature verifies the GitHub webhook signature.
func (h *Handler) verifyGitHubSignature(signature string, payload []byte, secret string) bool {
	if signature == "" || !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	// Extract hex signature
	sigHex := strings.TrimPrefix(signature, "sha256=")
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

// shouldTriggerDeploy determines if a push event should trigger a deployment.
func (h *Handler) shouldTriggerDeploy(event *PushEvent, targetBranch string, autoDeploy bool) bool {
	// Don't trigger for tags
	if event.IsTag || event.Branch == "" {
		return false
	}

	// Must have auto-deploy enabled
	if !autoDeploy {
		return false
	}

	// Branch must match
	return event.Branch == targetBranch
}

// processPushEvent processes a push event and logs it.
// Future implementation will trigger deployments based on project configuration.
func (h *Handler) processPushEvent(ctx context.Context, event *PushEvent) {
	// Log the event for now - actual deployment triggering would require
	// looking up the project by repository URL
	fmt.Printf("Received push event: repo=%s branch=%s commit=%s\n",
		event.RepositoryName, event.Branch, event.CommitSHA)

	// If deploy service is configured, we can look up projects and trigger deployments
	if h.deployService != nil {
		// TODO: Implement deployment triggering:
		// 1. Look up project by repository URL
		// 2. Check if auto-deploy is enabled
		// 3. Check if the branch matches the configured branch
		// 4. Create a new deployment with TriggerType = webhook
		_ = ctx // Context would be used for deployment operations
	}
}

// GitLabPushEvent represents a push event from GitLab.
type GitLabPushEvent struct {
	Ref           string `json:"ref"`
	CheckoutSHA   string `json:"checkout_sha"`
	UserName      string `json:"user_name"`
	ProjectID     int    `json:"project_id"`
	ProjectPath   string `json:"path_with_namespace"`
	ProjectURL    string `json:"project_url"`
	CommitMessage string `json:"-"` // Extracted from commits array
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

		fmt.Printf("GitLab push: project=%s ref=%s sha=%s\n",
			event.ProjectPath, event.Ref, event.CheckoutSHA)

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

// Config holds configuration for webhook handling.
type Config struct {
	Secret      string
	AllowedIPs  []string // Optional IP allowlist
	RateLimitPS int      // Rate limit per second (0 = no limit)
}

// DefaultConfig returns default webhook configuration.
func DefaultConfig() Config {
	return Config{
		RateLimitPS: 10, // 10 requests per second
	}
}
