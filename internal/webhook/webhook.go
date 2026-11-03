// Package webhook handles incoming webhooks from git providers.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/httputil"
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

// Webhook providers, written to details.provider on deployment.trigger.
const (
	ProviderGitHub = "github"
	ProviderGitea  = "gitea"
	ProviderGitLab = "gitlab"
)

// projectLister finds the projects built from a repository
// (repositories.ProjectRepository).
type projectLister interface {
	ListByRepoURL(ctx context.Context, repoURL string) ([]*models.Project, error)
}

// environmentLister lists a project's environments
// (repositories.EnvironmentRepository).
type environmentLister interface {
	ListByProjectID(ctx context.Context, projectID string) ([]*models.Environment, error)
}

// deploymentCreator stores a new deployment row (deploy.Service).
type deploymentCreator interface {
	CreateDeployment(ctx context.Context, d *models.Deployment) error
}

// deploymentEnqueuer queues a deployment; an audit id on ctx
// (models.WithAuditID) is kept with the queue item (deploy.Worker).
type deploymentEnqueuer interface {
	Enqueue(ctx context.Context, deploymentID string) error
}

// AuditRecorder writes an audit row with a chosen id before it returns
// (services.AuditService).
type AuditRecorder interface {
	RecordAs(ctx context.Context, id, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string)
}

// Handler handles webhook requests from git providers.
type Handler struct {
	deployService   deploymentCreator
	projectRepo     projectLister
	environmentRepo environmentLister
	worker          deploymentEnqueuer
	audit           AuditRecorder
	proxyTrust      httputil.ProxyTrust
	webhookSecret   string
	logger          *slog.Logger
}

// pushSource says where a push event came from, for the audit row.
type pushSource struct {
	provider  string
	ip        string
	userAgent string
}

// NewHandler creates a new webhook Handler.
func NewHandler(
	ds *deploy.Service,
	pr *repositories.ProjectRepository,
	er *repositories.EnvironmentRepository,
	w *deploy.Worker,
	secret string,
	logger *slog.Logger,
) *Handler {
	h := &Handler{webhookSecret: secret, logger: logger}
	// Keep nil pointers out of the interface fields so the nil checks in
	// processPushEvent still work.
	if ds != nil {
		h.deployService = ds
	}
	if pr != nil {
		h.projectRepo = pr
	}
	if er != nil {
		h.environmentRepo = er
	}
	if w != nil {
		h.worker = w
	}
	return h
}

// WithAudit records a deployment.trigger row (via=webhook) for every
// deployment a push starts, owned by the project's owner, and links the
// queued run to it so its deployment.run.* row connects. trust decides
// which proxy headers give the client IP.
func (h *Handler) WithAudit(a AuditRecorder, trust httputil.ProxyTrust) *Handler {
	h.audit = a
	h.proxyTrust = trust
	return h
}

// source describes the request for the audit row.
func (h *Handler) source(r *http.Request, provider string) pushSource {
	return pushSource{provider: provider, ip: httputil.ClientIP(r, h.proxyTrust), userAgent: r.UserAgent()}
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

	// Read body (limit to 1MB to prevent abuse)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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
		h.processPushEvent(r.Context(), h.source(r, ProviderGitHub), event)

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

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

		h.processPushEvent(r.Context(), h.source(r, ProviderGitea), event)
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "accepted"}`)

	default:
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, `{"status": "ignored"}`)
	}
}

// processPushEvent processes a push event and triggers deployments.
func (h *Handler) processPushEvent(ctx context.Context, src pushSource, event *PushEvent) {
	h.logger.Info("received push event",
		"repo", event.RepositoryName, "branch", event.Branch, "commit", event.CommitSHA)

	if !event.IsBranchPush() {
		return
	}

	if h.projectRepo == nil || h.environmentRepo == nil || h.deployService == nil || h.worker == nil {
		return
	}

	// 1. Look up projects by repository URL
	// We might need to normalize the URL (remove .git suffix, handle http/ssh differences)
	// For now, we assume exact match or simple normalization if needed.
	projects, err := h.projectRepo.ListByRepoURL(ctx, event.RepositoryURL)
	if err != nil {
		h.logger.Error("failed to list projects for repo", "repo", event.RepositoryURL, "error", err)
		return
	}

	if len(projects) == 0 {
		h.logger.Debug("no projects found for repo", "repo", event.RepositoryURL)
		return
	}

	// 2. Iterate projects and their environments
	for _, proj := range projects {
		envs, err := h.environmentRepo.ListByProjectID(ctx, proj.ID)
		if err != nil {
			h.logger.Error("failed to list environments for project", "project_id", proj.ID, "error", err)
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
			h.logger.Info("triggering deployment", "project", proj.Name, "environment", env.Name)

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
				h.logger.Error("failed to create deployment record", "error", err)
				continue
			}

			// Record who started it (the project owner, via the webhook) and
			// carry the row id to the queue so deployment.run.* links back.
			auditID := h.recordTrigger(ctx, src, proj, env, d, event)

			// Enqueue for background processing
			if err := h.worker.Enqueue(models.WithAuditID(ctx, auditID), d.ID); err != nil {
				h.logger.Error("failed to enqueue deployment", "deployment_id", d.ID, "error", err)
				h.recordTriggerFailed(ctx, src, proj, d, auditID, err)
			}
		}
	}
}

// maxAuditErrorLen caps the error kept on a .failed row.
const maxAuditErrorLen = 500

// recordTrigger writes deployment.trigger for a webhook deployment and
// returns its id ("" without a recorder).
func (h *Handler) recordTrigger(ctx context.Context, src pushSource, proj *models.Project, env *models.Environment, d *models.Deployment, event *PushEvent) string {
	if h.audit == nil {
		return ""
	}
	id := uuid.New().String()
	h.audit.RecordAs(ctx, id, proj.UserID, models.AuditActionDeploymentTrigger, models.ResourceTypeDeployment, d.ID,
		map[string]interface{}{
			"via":            models.AuditViaWebhook,
			"provider":       src.provider,
			"project_id":     proj.ID,
			"environment_id": env.ID,
			"repository":     event.RepositoryName,
			"branch":         event.Branch,
			"commit":         event.CommitSHA,
		}, src.ip, src.userAgent)
	return id
}

// recordTriggerFailed writes deployment.trigger.failed linked to auditID
// when the deployment could not be queued.
func (h *Handler) recordTriggerFailed(ctx context.Context, src pushSource, proj *models.Project, d *models.Deployment, auditID string, err error) {
	if h.audit == nil || auditID == "" {
		return
	}
	msg := err.Error()
	if len(msg) > maxAuditErrorLen {
		msg = strings.ToValidUTF8(msg[:maxAuditErrorLen-3], "") + "..."
	}
	h.audit.RecordAs(ctx, "", proj.UserID, models.AuditActionDeploymentTrigger+".failed", models.ResourceTypeDeployment, d.ID,
		map[string]interface{}{"via": models.AuditViaWebhook, "provider": src.provider, "audit_id": auditID, "error": msg},
		src.ip, src.userAgent)
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

	// GitLab sends a secret token in the header — use constant-time comparison
	token := r.Header.Get("X-Gitlab-Token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.webhookSecret)) != 1 {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-Gitlab-Event")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
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

		h.processPushEvent(r.Context(), h.source(r, ProviderGitLab), pushEvent)

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
