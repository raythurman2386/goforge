package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
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
	apiTokenRepo    *repositories.APITokenRepository
	backups         BackupStatusReader
	logger          *slog.Logger
}

// NewSettingsHandler creates a new SettingsHandler.
func NewSettingsHandler(gsr *repositories.GitSourceRepository, is *services.InstanceService, ur *repositories.UserRepository, atr *repositories.APITokenRepository, logger *slog.Logger) *SettingsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SettingsHandler{
		gitSourceRepo:   gsr,
		instanceService: is,
		userRepo:        ur,
		apiTokenRepo:    atr,
		logger:          logger,
	}
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
	layout := layouts.AppLayout("Add Git Source", csrfToken, nil)
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
	component := settings.Profile(currentUser, csrfToken, h.backupStatus(r, currentUser))
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

	for _, token := range tokens {
		if err := settings.APITokenRow(token).Render(r.Context(), w); err != nil {
			h.logger.Error("failed to render token row", "error", err)
		}
	}
}

// NewAPITokenModal renders the modal to create a new API token.
func (h *SettingsHandler) NewAPITokenModal(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	component := settings.AddAPITokenModal(csrfToken)

	if r.Header.Get("HX-Request") == htmxTrue {
		if err := component.Render(r.Context(), w); err != nil {
			http.Error(w, "failed to render modal", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	layout := layouts.AppLayout("Generate API Token", csrfToken, nil)
	if err := layout.Render(templ.WithChildren(r.Context(), component), w); err != nil {
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

// CreateAPIToken handles creating a new API token.
func (h *SettingsHandler) CreateAPIToken(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	name := r.FormValue("name")
	expiresIn := r.FormValue("expires_in")

	if name == "" {
		RenderError(w, r, http.StatusBadRequest, "Token name is required")
		return
	}
	if len(name) > maxTokenNameLen {
		RenderError(w, r, http.StatusBadRequest, "Token name must be under 100 characters")
		return
	}

	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "invalid form data")
		return
	}
	scopes, ok := models.NormalizeScopes(r.PostForm["scopes"])
	if !ok {
		RenderError(w, r, http.StatusBadRequest, "Unknown token scope")
		return
	}

	// Generate random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		h.logger.Error("failed to generate token", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to generate token")
		return
	}
	rawToken := hex.EncodeToString(tokenBytes)

	// Hash the token for storage
	tokenHash := hashToken(rawToken)

	// Calculate expiration
	var expiresAt sql.NullTime
	if expiresIn != "0" {
		days := 365
		if _, err := fmt.Sscanf(expiresIn, "%d", &days); err == nil {
			expiresAt = sql.NullTime{Time: time.Now().AddDate(0, 0, days), Valid: true}
		}
	}

	token := &models.APIToken{
		UserID:    user.ID,
		Name:      name,
		TokenHash: tokenHash,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}

	if err := h.apiTokenRepo.Create(r.Context(), token); err != nil {
		h.logger.Error("failed to create API token", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to create token")
		return
	}

	// Trigger a token list refresh on the client
	w.Header().Set("HX-Trigger", "tokenCreated")

	// Return the raw token to the user (only shown once)
	w.WriteHeader(http.StatusCreated)
	if err := settings.TokenCreatedModal(rawToken).Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render token created modal", "error", err)
	}
}

// DeleteAPIToken handles deleting an API token.
func (h *SettingsHandler) DeleteAPIToken(w http.ResponseWriter, r *http.Request) {
	val := r.Context().Value(auth.UserContextKey)
	user, ok := val.(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}

	id := chi.URLParam(r, "id")

	// Verify token belongs to user
	token, err := h.apiTokenRepo.GetByID(r.Context(), id)
	if err != nil {
		RenderError(w, r, http.StatusNotFound, "Token not found")
		return
	}

	if token.UserID != user.ID {
		RenderError(w, r, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := h.apiTokenRepo.Delete(r.Context(), id); err != nil {
		h.logger.Error("failed to delete API token", "error", err, "token_id", id)
		RenderError(w, r, http.StatusInternalServerError, "Failed to delete token")
		return
	}

	// Re-render the full token list after deletion
	h.ListAPITokens(w, r)
}

// hashToken creates a SHA-256 hash of an API token for storage.
// SHA-256 is appropriate here because API tokens are high-entropy,
// cryptographically random values (32 bytes / 256 bits) that are not
// susceptible to dictionary attacks.
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
