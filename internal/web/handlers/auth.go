package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

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
	githubProvider     GitHubOAuth
	authenticator      auth.Authenticator
	auditService       AuthAuditRecorder
	logger             *slog.Logger
	enableRegistration bool
	loginThrottle      LoginThrottle
	twoFactor          *auth.TwoFactor
}

// GitHubOAuth is the GitHub OAuth client the handler uses.
// *auth.GitHubProvider implements it; tests use a fake.
type GitHubOAuth interface {
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	GetUser(ctx context.Context, token *oauth2.Token) (*github.User, error)
}

// AuthAuditRecorder writes sign-in audit rows. *services.AuditService
// implements it.
type AuthAuditRecorder interface {
	Record(ctx context.Context, userID, action, resourceType, resourceID string, details map[string]interface{}, ip, userAgent string)
}

// WithTwoFactor turns on the TOTP step for password sign-in of accounts
// that enrolled. GitHub OAuth sign-in never uses it.
func (h *AuthHandler) WithTwoFactor(tf *auth.TwoFactor) *AuthHandler {
	h.twoFactor = tf
	return h
}

// WithGitHubProvider replaces the GitHub OAuth client (tests).
func (h *AuthHandler) WithGitHubProvider(gp GitHubOAuth) *AuthHandler {
	h.githubProvider = gp
	return h
}

// WithAuditRecorder replaces the audit recorder (tests).
func (h *AuthHandler) WithAuditRecorder(a AuthAuditRecorder) *AuthHandler {
	h.auditService = a
	return h
}

// record writes an audit row when a recorder is set.
func (h *AuthHandler) record(r *http.Request, userID, action string, details map[string]interface{}) {
	if h.auditService == nil {
		return
	}
	h.auditService.Record(r.Context(), userID, action, models.ResourceTypeUser, userID, details, getClientIP(r), r.UserAgent())
}

// LoginThrottle limits repeated failed password logins. See
// middleware.LoginThrottle.
type LoginThrottle interface {
	Check(ip, account string) (retryAfter time.Duration, allowed bool)
	Failure(ip, account string)
	Success(account string)
}

// WithLoginThrottle enables per-IP and per-account lockout on POST /auth/login.
func (h *AuthHandler) WithLoginThrottle(t LoginThrottle) *AuthHandler {
	h.loginThrottle = t
	return h
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(sm *auth.SessionManager, ur *repositories.UserRepository, gp *auth.GitHubProvider, a auth.Authenticator, as *services.AuditService, enableRegistration bool, logger *slog.Logger) *AuthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &AuthHandler{
		sessionManager:     sm,
		userRepo:           ur,
		authenticator:      a,
		logger:             logger,
		enableRegistration: enableRegistration,
	}
	// Leave the interfaces nil rather than holding a typed nil pointer.
	if gp != nil {
		h.githubProvider = gp
	}
	if as != nil {
		h.auditService = as
	}
	return h
}

// LoginPage renders the login page.
func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard
	if r.Context().Value(auth.UserContextKey) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := pages.Login(csrfToken, "", h.registrationAllowed(r.Context()))
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render login page")
	}
}

// Login handles the login form submission.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	rememberMe := r.FormValue("remember_me") == "on"

	clientIP := getClientIP(r)
	if h.loginThrottle != nil {
		// Fail closed: a locked IP or account is refused before the password
		// is checked, so a correct guess during the lockout does not succeed.
		if retryAfter, ok := h.loginThrottle.Check(clientIP, email); !ok {
			h.logger.Warn("login throttled", "client_ip", clientIP)
			secs := int(math.Ceil(retryAfter.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			csrfToken := auth.GetCSRFToken(r)
			component := pages.Login(csrfToken, "Too many failed sign-in attempts. Try again later.", h.registrationAllowed(r.Context()))
			w.WriteHeader(http.StatusTooManyRequests)
			if renderErr := component.Render(r.Context(), w); renderErr != nil {
				h.logger.Error("failed to render login page", "error", renderErr)
			}
			return
		}
	}

	user, err := h.authenticator.Authenticate(r.Context(), email, password)
	if err != nil {
		if h.loginThrottle != nil {
			h.loginThrottle.Failure(clientIP, email)
		}
		csrfToken := auth.GetCSRFToken(r)
		component := pages.Login(csrfToken, "Invalid email or password", h.registrationAllowed(r.Context()))
		w.WriteHeader(http.StatusUnauthorized)
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			RenderError(w, r, http.StatusInternalServerError, "failed to render login page")
		}
		return
	}

	// 2FA on: the password was right, but no session yet. Hand the browser a
	// short-lived challenge and ask for the code. The throttle is not cleared
	// here, so wrong codes keep counting toward the same lockout.
	if h.twoFactor != nil {
		on, tfErr := h.twoFactor.Enabled(r.Context(), user.ID)
		if tfErr != nil {
			// Fail closed: no session when the 2FA state cannot be read.
			h.logger.Error("failed to read two-factor state", "error", tfErr)
			RenderError(w, r, http.StatusInternalServerError, "Sign-in failed. Please try again.")
			return
		}
		if on {
			token, _, chErr := h.twoFactor.StartChallenge(r.Context(), user.ID, rememberMe)
			if chErr != nil {
				h.logger.Error("failed to start two-factor challenge", "error", chErr)
				RenderError(w, r, http.StatusInternalServerError, "Sign-in failed. Please try again.")
				return
			}
			auth.SetTwoFactorCookie(w, r, token)
			http.Redirect(w, r, "/auth/2fa", http.StatusSeeOther)
			return
		}
	}

	if h.loginThrottle != nil {
		h.loginThrottle.Success(email)
	}

	h.finishLogin(w, r, user, rememberMe, nil)
}

// finishLogin creates the session, sets its cookie, writes the login audit
// row, and redirects to the dashboard.
func (h *AuthHandler) finishLogin(w http.ResponseWriter, r *http.Request, user *models.User, rememberMe bool, details map[string]interface{}) {
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, rememberMe)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to create session")
		return
	}

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, auth.IsSecureRequest(r))

	h.record(r, user.ID, models.AuditActionLogin, details)

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// RegisterPage renders the registration page.
func (h *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	if !h.registrationAllowed(r.Context()) {
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
	if !h.registrationAllowed(r.Context()) {
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

	h.record(r, user.ID, models.AuditActionCreate, nil)
	h.record(r, user.ID, models.AuditActionLogin, nil)

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, auth.IsSecureRequest(r))
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// registrationOpen is true while no account exists, and otherwise follows the
// registration flag. A failed count does not open registration by itself.
func registrationOpen(userCount int, countErr error, enableRegistration bool) bool {
	if countErr != nil {
		return enableRegistration
	}
	if userCount == 0 {
		return true
	}
	return enableRegistration
}

// registrationAllowed is true for the first account even when open registration
// is disabled, and otherwise follows GOFORGE_ENABLE_REGISTRATION.
func (h *AuthHandler) registrationAllowed(ctx context.Context) bool {
	if h.userRepo == nil {
		return h.enableRegistration
	}
	n, err := h.userRepo.Count(ctx)
	if err != nil && h.logger != nil {
		h.logger.Error("failed to count users", "error", err)
	}
	return registrationOpen(n, err, h.enableRegistration)
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
			if !h.registrationAllowed(r.Context()) {
				RenderError(w, r, http.StatusForbidden, "Registration is disabled")
				return
			}
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

	// Create session. GitHub sign-in never asks for a GoForge TOTP code, even
	// when this account turned 2FA on for password sign-in: it relies on
	// GitHub's own 2FA (v0.5 S4 lock; see SECURITY.md).
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, true)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to create session")
		return
	}

	h.record(r, user.ID, models.AuditActionLogin, map[string]interface{}{"provider": "github"})

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
				h.record(r, user.ID, models.AuditActionLogout, nil)
			}
		}
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
