package handlers

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
	"github.com/raythurman2386/goforge/internal/web/templates/pages"
)

// AuthHandler handles authentication related requests.
type AuthHandler struct {
	sessionManager     *auth.SessionManager
	userRepo           *repositories.UserRepository
	githubProvider     *auth.GitHubProvider
	authenticator      auth.Authenticator
	auditService       *services.AuditService
	logger             *slog.Logger
	enableRegistration bool
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(sm *auth.SessionManager, ur *repositories.UserRepository, gp *auth.GitHubProvider, a auth.Authenticator, as *services.AuditService, enableRegistration bool, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{
		sessionManager:     sm,
		userRepo:           ur,
		githubProvider:     gp,
		authenticator:      a,
		auditService:       as,
		logger:             logger,
		enableRegistration: enableRegistration,
	}
}

// LoginPage renders the login page.
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard
	if r.Context().Value(auth.UserContextKey) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages.Login(csrfToken, "", h.enableRegistration)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render login page")
	}
}

// Login handles the login form submission.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	rememberMe := r.FormValue("remember_me") == "on"

	user, err := h.authenticator.Authenticate(r.Context(), email, password)
	if err != nil {
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Login(csrfToken, "Invalid email or password", h.enableRegistration)
		w.WriteHeader(http.StatusUnauthorized)
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render login page")
		}
		return
	}

	// Create session
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, rememberMe)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to create session")
		return
	}

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, auth.IsSecureRequest(r))

	h.auditService.Record(r.Context(), user.ID, models.AuditActionLogin, models.ResourceTypeUser, user.ID, nil, getClientIP(r), r.UserAgent())

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// RegisterPage renders the registration page.
func (h *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if !h.enableRegistration {
		RenderError(w, r, http.StatusForbidden, "Registration is disabled")
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages.Register(csrfToken, "")
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render register page")
	}
}

// Register handles the registration form submission.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.enableRegistration {
		RenderError(w, r, http.StatusForbidden, "Registration is disabled")
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if !isValidEmail(email) {
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Register(csrfToken, "Please enter a valid email address")
		w.WriteHeader(http.StatusBadRequest)
		if err := component.Render(r.Context(), w); err != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render register page")
		}
		return
	}

	if password != confirmPassword {
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Register(csrfToken, "Passwords do not match")
		w.WriteHeader(http.StatusBadRequest)
		if err := component.Render(r.Context(), w); err != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render register page")
		}
		return
	}

	if len(password) < 8 {
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Register(csrfToken, "Password must be at least 8 characters")
		w.WriteHeader(http.StatusBadRequest)
		if err := component.Render(r.Context(), w); err != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render register page")
		}
		return
	}

	if len(password) > maxPasswordLen {
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Register(csrfToken, "Password must be at most 256 characters")
		w.WriteHeader(http.StatusBadRequest)
		if err := component.Render(r.Context(), w); err != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render register page")
		}
		return
	}

	// Check if user already exists
	_, err := h.userRepo.GetByEmail(r.Context(), email)
	if err == nil {
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Register(csrfToken, "User already exists")
		w.WriteHeader(http.StatusBadRequest)
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render register page")
		}
		return
	}

	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to hash password")
		return
	}

	user := &models.User{
		Email:        email,
		PasswordHash: sql.NullString{String: hashedPassword, Valid: true},
	}

	if dbErr := h.userRepo.Create(r.Context(), user); dbErr != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Auto-login after registration
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, false)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	h.auditService.Record(r.Context(), user.ID, models.AuditActionCreate, models.ResourceTypeUser, user.ID, nil, getClientIP(r), r.UserAgent())
	h.auditService.Record(r.Context(), user.ID, models.AuditActionLogin, models.ResourceTypeUser, user.ID, nil, getClientIP(r), r.UserAgent())

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, auth.IsSecureRequest(r))
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// GitHubLogin initiates the GitHub OAuth flow.
func (h *AuthHandler) GitHubLogin(w http.ResponseWriter, r *http.Request) {
	state := uuid.New().String()
	// Store state in a cookie for validation in callback
	http.SetCookie(w, &http.Cookie{
		Name:     "github_oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   auth.IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   300, // 5 minutes
	})

	url := h.githubProvider.AuthURL(state)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// GitHubCallback handles the GitHub OAuth callback.
func (h *AuthHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("github_oauth_state")
	if err != nil || stateCookie.Value != r.FormValue("state") {
		RenderError(w, r, http.StatusBadRequest, "invalid oauth state")
		return
	}

	code := r.FormValue("code")
	token, err := h.githubProvider.Exchange(r.Context(), code)
	if err != nil {
		h.logger.Error("failed to exchange token", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Authentication failed. Please try again.")
		return
	}

	ghUser, err := h.githubProvider.GetUser(r.Context(), token)
	if err != nil {
		h.logger.Error("failed to get user info from GitHub", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Authentication failed. Please try again.")
		return
	}

	// Find or create user
	user, err := h.userRepo.GetByGitHubID(r.Context(), ghUser.GetID())
	if err != nil {
		// Not found by GitHub ID — check if an account with this email already exists
		email := ghUser.GetEmail()
		if email == "" {
			email = fmt.Sprintf("%s@github.com", ghUser.GetLogin())
		}

		user, err = h.userRepo.GetByEmail(r.Context(), email)
		if err == nil {
			// Existing user found by email — link their GitHub account
			user.GitHubID = sql.NullInt64{Int64: ghUser.GetID(), Valid: true}
			user.GitHubUsername = sql.NullString{String: ghUser.GetLogin(), Valid: true}
			user.AvatarURL = sql.NullString{String: ghUser.GetAvatarURL(), Valid: true}
			user.GitHubAccessToken = sql.NullString{String: token.AccessToken, Valid: true}
			if dbErr := h.userRepo.Update(r.Context(), user); dbErr != nil {
				h.logger.Error("failed to link GitHub account to existing user", "error", dbErr)
				RenderError(w, r, http.StatusInternalServerError, "Authentication failed. Please try again.")
				return
			}
		} else {
			// No existing user — create a new one
			user = &models.User{
				Email:             email,
				GitHubID:          sql.NullInt64{Int64: ghUser.GetID(), Valid: true},
				GitHubUsername:    sql.NullString{String: ghUser.GetLogin(), Valid: true},
				AvatarURL:         sql.NullString{String: ghUser.GetAvatarURL(), Valid: true},
				GitHubAccessToken: sql.NullString{String: token.AccessToken, Valid: true},
			}

			if dbErr := h.userRepo.Create(r.Context(), user); dbErr != nil {
				h.logger.Error("failed to create user from GitHub login", "error", dbErr)
				RenderError(w, r, http.StatusInternalServerError, "Authentication failed. Please try again.")
				return
			}
		}
	}

	// Create session
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, true)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to create session")
		return
	}

	h.auditService.Record(r.Context(), user.ID, models.AuditActionLogin, models.ResourceTypeUser, user.ID, map[string]interface{}{"provider": "github"}, getClientIP(r), r.UserAgent())

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, auth.IsSecureRequest(r))
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Logout clears the session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := auth.GetSessionToken(r)
	if token != "" {
		if err := h.sessionManager.DestroySession(r.Context(), token); err != nil {
			// Log error but continue with clearing cookie
			h.logger.Warn("failed to destroy session", "error", err)
		} else {
			// Try to get user from context if possible, but context might not have it if middleware failed?
			// Actually middleware puts it there.
			if user := auth.GetUserFromContext(r.Context()); user != nil {
				h.auditService.Record(r.Context(), user.ID, models.AuditActionLogout, models.ResourceTypeUser, user.ID, nil, getClientIP(r), r.UserAgent())
			}
		}
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
