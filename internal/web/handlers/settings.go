package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/layouts"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

// SettingsHandler handles settings related requests.
type SettingsHandler struct {
	gitSourceRepo   *repositories.GitSourceRepository
	instanceService *services.InstanceService
	userRepo        *repositories.UserRepository
	apiTokenRepo    APITokenStore
	mcp             *settings.MCPCard
	backups         BackupStatusReader
	backupSettings  BackupSettings
	restorePoints   RestorePointLister
	notifySettings  NotifySettings
	errReport       ErrorReportingSettings
	audit           AuditRecorder
	tokenProjects   TokenProjectLister
	tokenServices   TokenServiceLister
	logger          *slog.Logger
}

// TokenProjectLister lists the owner's projects for token restriction
// (project.Service).
type TokenProjectLister interface {
	ListProjects(ctx context.Context, userID string) ([]*models.Project, error)
}

// TokenServiceLister lists the owner's service instances for token
// restriction (services.InstanceService).
type TokenServiceLister interface {
	ListInstances(ctx context.Context, userID string) ([]*models.ServiceInstance, error)
}

// WithTokenResources lets the token form restrict a token to the owner's
// projects and services. Without it the form has no restriction section
// and a posted project_ids or service_ids is refused.
func (h *SettingsHandler) WithTokenResources(p TokenProjectLister, s TokenServiceLister) *SettingsHandler {
	h.tokenProjects = p
	h.tokenServices = s
	return h
}

// tokenResources loads the user's projects and services for the token
// form and list. ok is false when either list cannot be read.
func (h *SettingsHandler) tokenResources(ctx context.Context, userID string) (*settings.TokenResources, bool) {
	if h.tokenProjects == nil || h.tokenServices == nil {
		return nil, false
	}
	projects, err := h.tokenProjects.ListProjects(ctx, userID)
	if err != nil {
		h.logger.Warn("token form: could not list projects", "error", err)
		return nil, false
	}
	insts, err := h.tokenServices.ListInstances(ctx, userID)
	if err != nil {
		h.logger.Warn("token form: could not list services", "error", err)
		return nil, false
	}
	res := &settings.TokenResources{}
	for _, p := range projects {
		if p != nil && p.UserID == userID {
			res.Projects = append(res.Projects, settings.TokenResource{ID: strings.ToLower(p.ID), Name: p.Name})
		}
	}
	for _, in := range insts {
		if in != nil && in.UserID == userID {
			res.Services = append(res.Services, settings.TokenResource{ID: strings.ToLower(in.ID), Name: in.Name})
		}
	}
	return res, true
}

// ownsAll reports whether every id is one of the owner's resources.
func ownsAll(ids []string, owned []settings.TokenResource) bool {
	for _, id := range ids {
		if !slices.ContainsFunc(owned, func(r settings.TokenResource) bool { return r.ID == id }) {
			return false
		}
	}
	return true
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(gsr *repositories.GitSourceRepository, is *services.InstanceService, ur *repositories.UserRepository, atr *repositories.APITokenRepository, logger *slog.Logger) *SettingsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &SettingsHandler{
		gitSourceRepo:   gsr,
		instanceService: is,
		userRepo:        ur,
		logger:          logger,
	}
	if atr != nil {
		h.apiTokenRepo = atr
	}
	return h
}

// APITokenStore is the API token storage the Profile token routes use
// (repositories.APITokenRepository).
type APITokenStore interface {
	Create(ctx context.Context, token *models.APIToken) error
	GetByID(ctx context.Context, id string) (*models.APIToken, error)
	ListByUserID(ctx context.Context, userID string) ([]*models.APIToken, error)
	Rotate(ctx context.Context, id, userID, tokenHash, tokenHint string) error
	Delete(ctx context.Context, id string) error
}

// WithAPITokens replaces the token store (tests).
func (h *SettingsHandler) WithAPITokens(s APITokenStore) *SettingsHandler {
	h.apiTokenRepo = s
	return h
}

// WithAudit records API token create, rotate, and revoke in the audit log.
func (h *SettingsHandler) WithAudit(a AuditRecorder) *SettingsHandler {
	h.audit = a
	return h
}

// WithMCP shows the MCP card on the Profile page when /mcp is served.
// baseURL is GOFORGE_BASE_URL; the endpoint is baseURL + "/mcp".
func (h *SettingsHandler) WithMCP(baseURL string, enabled bool) *SettingsHandler {
	h.mcp = nil
	if enabled {
		h.mcp = settings.NewMCPCard(baseURL)
	}
	return h
}

// GitSources renders the git sources settings page.
func (h *SettingsHandler) GitSources(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	sources, err := h.gitSourceRepo.ListByUserID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to list git sources")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := settings.GitSources(sources, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to render settings page")
	}
}

// NewGitSourceModal renders the modal to add a new git source.
func (h *SettingsHandler) NewGitSourceModal(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	component := settings.AddGitSourceModal(csrfToken)

	if r.Header.Get("HX-Request") == htmxTrue {
		if err := component.Render(r.Context(), w); err != nil {
			http.Error(w, "failed to render modal", http.StatusInternalServerError)
		}
		return
	}

	// For direct access, wrap in AppLayout
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	layout := layouts.AppLayout("Add Git Source", csrfToken)
	if err := layout.Render(templ.WithChildren(r.Context(), component), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

// CreateGitSource handles creating a new git source.
func (h *SettingsHandler) CreateGitSource(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	name := r.FormValue("name")
	sourceType := r.FormValue("type")
	apiURL := r.FormValue("api_url")
	accessToken := r.FormValue("access_token")

	if name == "" || len(name) > maxInputLength {
		http.Error(w, "name is required and must be under 255 characters", http.StatusBadRequest)
		return
	}

	switch models.GitSourceType(sourceType) {
	case models.GitSourceTypeGitHub, models.GitSourceTypeGitLab,
		models.GitSourceTypeGitea, models.GitSourceTypeSSH:
		// valid
	default:
		http.Error(w, "invalid git source type", http.StatusBadRequest)
		return
	}

	source := &models.GitSource{
		UserID:      user.ID,
		Name:        name,
		Type:        models.GitSourceType(sourceType),
		AccessToken: sql.NullString{String: accessToken, Valid: accessToken != ""},
	}
	if apiURL != "" {
		source.APIURL = sql.NullString{String: apiURL, Valid: true}
	}

	if err := h.gitSourceRepo.Create(r.Context(), source); err != nil {
		http.Error(w, "failed to create git source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings/git")
	w.WriteHeader(http.StatusCreated)
}

// DeleteGitSource handles deleting a git source.
func (h *SettingsHandler) DeleteGitSource(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	id := chi.URLParam(r, "id")

	// Verify the git source belongs to the authenticated user
	source, err := h.gitSourceRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "git source not found", http.StatusNotFound)
		return
	}
	if source.UserID != user.ID {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	if err := h.gitSourceRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete git source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Redirect", "/settings/git")
	w.WriteHeader(http.StatusOK)
}

// PruneSystem triggers a system prune.
func (h *SettingsHandler) PruneSystem(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}

	if err := h.instanceService.PruneSystem(r.Context()); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to prune system")
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := components.Toast("System pruned successfully", "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render prune toast", "error", err)
	}
}

// BackupStatusReader reads the backup status for the Settings card.
type BackupStatusReader interface {
	Status(ctx context.Context) (*backup.Status, error)
}

// WithBackups enables the admin-only Backups card on the profile page.
func (h *SettingsHandler) WithBackups(b BackupStatusReader) *SettingsHandler {
	h.backups = b
	return h
}

// backupStatus returns nil (card hidden) for non-admins or on error.
func (h *SettingsHandler) backupStatus(r *http.Request, user *models.User) *backup.Status {
	if h.backups == nil || !user.IsAdmin() {
		return nil
	}
	st, err := h.backups.Status(r.Context())
	if err != nil {
		h.logger.Error("failed to read backup status", "error", err)
		return nil
	}
	return st
}

// Profile renders the profile settings page.
func (h *SettingsHandler) Profile(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	// Fetch fresh user data
	currentUser, err := h.userRepo.GetByID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to load profile")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	st := h.backupStatus(r, currentUser)
	var form *backup.SettingsView
	if st != nil {
		form = h.backupForm(r)
	}
	// The page carries backup settings for admins; never cache it.
	w.Header().Set("Cache-Control", "no-store")
	component := settings.Profile(currentUser, csrfToken, st, form, h.notifyForm(r, currentUser), h.errReportForm(r, currentUser), h.mcp)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to render profile page")
	}
}

// UpdateProfile handles updating profile information.
func (h *SettingsHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	email := r.FormValue("email")
	if email == "" {
		RenderError(w, r, http.StatusBadRequest, "Email is required")
		return
	}

	if !isValidEmail(email) {
		RenderError(w, r, http.StatusBadRequest, "Please enter a valid email address")
		return
	}

	// Update user email
	if err := h.userRepo.UpdateEmail(r.Context(), user.ID, email); err != nil {
		h.logger.Error("failed to update user email", "error", err, "user_id", user.ID)
		RenderError(w, r, http.StatusInternalServerError, "Failed to update email")
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := components.Toast("Profile updated successfully", "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}

// UpdatePassword handles changing the user's password.
func (h *SettingsHandler) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		RenderError(w, r, http.StatusBadRequest, "All password fields are required")
		return
	}

	if newPassword != confirmPassword {
		RenderError(w, r, http.StatusBadRequest, "New passwords do not match")
		return
	}

	if len(newPassword) < 8 {
		RenderError(w, r, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}

	if len(newPassword) > maxPasswordLen {
		RenderError(w, r, http.StatusBadRequest, "Password must be at most 256 characters")
		return
	}

	// Verify current password
	dbUser, err := h.userRepo.GetByID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to verify password")
		return
	}

	valid, compareErr := auth.ComparePassword(currentPassword, dbUser.PasswordHash.String)
	if compareErr != nil || !valid {
		RenderError(w, r, http.StatusBadRequest, "Current password is incorrect")
		return
	}

	// Hash new password and update
	hashedPassword, err := auth.HashPassword(newPassword)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	if err := h.userRepo.UpdatePassword(r.Context(), user.ID, hashedPassword); err != nil {
		h.logger.Error("failed to update password", "error", err, "user_id", user.ID)
		RenderError(w, r, http.StatusInternalServerError, "Failed to update password")
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := components.Toast("Password updated successfully", "success").Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render toast", "error", err)
	}
}

// ListAPITokens renders the list of API tokens.
func (h *SettingsHandler) ListAPITokens(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	tokens, err := h.apiTokenRepo.ListByUserID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to list tokens")
		return
	}

	if len(tokens) == 0 {
		if err := settings.EmptyTokenList().Render(r.Context(), w); err != nil {
			h.logger.Error("failed to render empty token list", "error", err)
		}
		return
	}

	var res *settings.TokenResources
	if slices.ContainsFunc(tokens, (*models.APIToken).IsRestricted) {
		res, _ = h.tokenResources(r.Context(), user.ID)
	}
	for _, token := range tokens {
		if err := settings.APITokenRow(token, res).Render(r.Context(), w); err != nil {
			h.logger.Error("failed to render token row", "error", err)
		}
	}
}

// NewAPITokenModal renders the modal to create a new API token.
// ?preset=mcp opens the MCP preset: read and write checked, 90 days, no
// full or never-expiring option.
func (h *SettingsHandler) NewAPITokenModal(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	var res *settings.TokenResources
	if user, ok := r.Context().Value(auth.UserContextKey).(*models.User); ok {
		res, _ = h.tokenResources(r.Context(), user.ID)
	}
	component := settings.AddAPITokenModal(csrfToken, r.URL.Query().Get("preset") == mcpPreset, res)

	if r.Header.Get("HX-Request") == htmxTrue {
		if err := component.Render(r.Context(), w); err != nil {
			http.Error(w, "failed to render modal", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	layout := layouts.AppLayout("Generate API Token", csrfToken)
	if err := layout.Render(templ.WithChildren(r.Context(), component), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

const (
	mcpPreset = "mcp"
	// mcpDefaultDays is the MCP preset's default lifetime.
	mcpDefaultDays = 90
	// defaultTokenDays is the lifetime when expires_in is missing.
	defaultTokenDays = 365
)

// tokenLifetimes are the expires_in choices, in days. 0 is never (not
// offered by the MCP preset).
var tokenLifetimes = []int{30, 90, 365}

// CreateAPIToken handles creating a new API token.
func (h *SettingsHandler) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "invalid form data")
		return
	}
	name := r.PostFormValue("name")
	if name == "" {
		RenderError(w, r, http.StatusBadRequest, "Token name is required")
		return
	}
	if len(name) > maxTokenNameLen {
		RenderError(w, r, http.StatusBadRequest, "Token name must be under 100 characters")
		return
	}
	scopes, ok := models.NormalizeScopes(r.PostForm["scopes"])
	if !ok {
		RenderError(w, r, http.StatusBadRequest, "Unknown token scope")
		return
	}

	preset := r.PostFormValue("preset") == mcpPreset
	days := defaultTokenDays
	if preset {
		days = mcpDefaultDays
		if !slices.ContainsFunc(r.PostForm["scopes"], func(v string) bool { return v != "" }) {
			scopes = []string{models.ScopeRead, models.ScopeWrite}
		}
		if !(&models.APIToken{Scopes: scopes}).IsMCPCapable() {
			RenderError(w, r, http.StatusBadRequest, "An MCP token needs read or write and cannot be full")
			return
		}
	}
	if v := r.PostFormValue("expires_in"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err == nil && n == 0 && !preset:
			days = 0
		case err == nil && slices.Contains(tokenLifetimes, n):
			days = n
		case preset && n == 0:
			RenderError(w, r, http.StatusBadRequest, "An MCP token must expire")
			return
		default:
			RenderError(w, r, http.StatusBadRequest, "Unknown expiry")
			return
		}
	}
	projectIDs, serviceIDs, status, msg := h.tokenResourceLists(r, user.ID, scopes)
	if status != 0 {
		RenderError(w, r, status, msg)
		return
	}
	var expiresAt sql.NullTime
	if days > 0 {
		expiresAt = sql.NullTime{Time: time.Now().AddDate(0, 0, days), Valid: true}
	}

	rawToken, tokenHash, hint, err := auth.GenerateAPIToken()
	if err != nil {
		h.logger.Error("failed to generate token", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	token := &models.APIToken{
		UserID:     user.ID,
		Name:       name,
		TokenHash:  tokenHash,
		TokenHint:  hint,
		Scopes:     scopes,
		ProjectIDs: projectIDs,
		ServiceIDs: serviceIDs,
		ExpiresAt:  expiresAt,
		CreatedAt:  time.Now(),
	}

	if err := h.apiTokenRepo.Create(r.Context(), token); err != nil {
		h.logger.Error("failed to create API token", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to create token")
		return
	}
	details := tokenAuditDetails(token)
	details["mcp_preset"] = preset
	h.recordToken(r, user.ID, models.AuditActionAPITokenCreate, token.ID, details)

	// Trigger a token list refresh on the client
	w.Header().Set("HX-Trigger", "tokenCreated")
	w.Header().Set("Cache-Control", "no-store")

	// Return the raw token to the user (only shown once)
	w.WriteHeader(http.StatusCreated)
	if err := settings.TokenCreatedModal("API Token Created", rawToken, token.IsMCPCapable()).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render token created modal", "error", err)
	}
}

// tokenResourceLists reads the form's project_ids and service_ids for a new
// token. Every ID must be one of the user's own projects or services, and a
// restricted token needs explicit scopes. A non-zero status refuses the form
// with msg.
func (h *SettingsHandler) tokenResourceLists(r *http.Request, userID string, scopes []string) (projectIDs, serviceIDs []string, status int, msg string) {
	projectIDs, okP := models.NormalizeResourceIDs(r.PostForm["project_ids"])
	serviceIDs, okS := models.NormalizeResourceIDs(r.PostForm["service_ids"])
	if !okP || !okS {
		return nil, nil, http.StatusBadRequest, "Unknown project or service"
	}
	if len(projectIDs) == 0 && len(serviceIDs) == 0 {
		return projectIDs, serviceIDs, 0, ""
	}
	if (&models.APIToken{Scopes: scopes}).IsFull() {
		return nil, nil, http.StatusBadRequest, "A token restricted to projects or services needs explicit scopes"
	}
	res, ok := h.tokenResources(r.Context(), userID)
	if !ok {
		return nil, nil, http.StatusServiceUnavailable, "Could not check your projects and services; try again"
	}
	if !ownsAll(projectIDs, res.Projects) || !ownsAll(serviceIDs, res.Services) {
		return nil, nil, http.StatusBadRequest, "Unknown project or service"
	}
	return projectIDs, serviceIDs, 0, ""
}

// ownedToken loads the {id} token if it belongs to the session user. Any
// other token answers 404, so ids of other users' tokens are not confirmed.
func (h *SettingsHandler) ownedToken(w http.ResponseWriter, r *http.Request) (*models.User, *models.APIToken, bool) {
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return nil, nil, false
	}
	token, err := h.apiTokenRepo.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil || token == nil || token.UserID != user.ID {
		RenderError(w, r, http.StatusNotFound, "Token not found")
		return nil, nil, false
	}
	return user, token, true
}

// RotateAPIToken replaces a token's secret on the same row: the id, name,
// scopes, and expiry stay; the old secret stops working at once. The new
// token is shown once.
func (h *SettingsHandler) RotateAPIToken(w http.ResponseWriter, r *http.Request) {
	user, token, ok := h.ownedToken(w, r)
	if !ok {
		return
	}
	if token.ExpiresAt.Valid && !token.IsActive() {
		RenderError(w, r, http.StatusBadRequest, "This token has expired; generate a new one instead")
		return
	}
	rawToken, tokenHash, hint, err := auth.GenerateAPIToken()
	if err != nil {
		h.logger.Error("failed to generate token", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to generate token")
		return
	}
	if err := h.apiTokenRepo.Rotate(r.Context(), token.ID, user.ID, tokenHash, hint); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			RenderError(w, r, http.StatusNotFound, "Token not found")
			return
		}
		h.logger.Error("failed to rotate API token", "error", err, "token_id", token.ID)
		RenderError(w, r, http.StatusInternalServerError, "Failed to rotate token")
		return
	}
	h.recordToken(r, user.ID, models.AuditActionAPITokenRotate, token.ID, tokenAuditDetails(token))

	w.Header().Set("HX-Trigger", "tokenCreated")
	w.Header().Set("Cache-Control", "no-store")
	if err := settings.TokenCreatedModal("API Token Rotated", rawToken, token.IsMCPCapable()).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render token rotated modal", "error", err)
	}
}

// DeleteAPIToken revokes (deletes) an API token.
func (h *SettingsHandler) DeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	user, token, ok := h.ownedToken(w, r)
	if !ok {
		return
	}
	if err := h.apiTokenRepo.Delete(r.Context(), token.ID); err != nil {
		h.logger.Error("failed to delete API token", "error", err, "token_id", token.ID)
		RenderError(w, r, http.StatusInternalServerError, "Failed to delete token")
		return
	}
	h.recordToken(r, user.ID, models.AuditActionAPITokenRevoke, token.ID, tokenAuditDetails(token))

	// Re-render the full token list after deletion
	h.ListAPITokens(w, r)
}

// tokenAuditDetails is what an audit row says about a token: never the
// token, its hash, or its hint.
func tokenAuditDetails(t *models.APIToken) map[string]interface{} {
	d := map[string]interface{}{"name": t.Name, "scopes": tokenScopeList(t)}
	if t.IsRestricted() {
		d["project_ids"] = append([]string{}, t.ProjectIDs...)
		d["service_ids"] = append([]string{}, t.ServiceIDs...)
	}
	if t.ExpiresAt.Valid {
		d["expires_at"] = t.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	return d
}

func tokenScopeList(t *models.APIToken) []string {
	if t.IsFull() {
		return []string{models.ScopeFull}
	}
	return append([]string(nil), t.Scopes...)
}

func (h *SettingsHandler) recordToken(r *http.Request, userID, action, tokenID string, details map[string]interface{}) {
	if h.audit == nil {
		return
	}
	h.audit.Record(r.Context(), userID, action, models.ResourceTypeAPIToken, tokenID, details, getClientIP(r), r.UserAgent())
}
