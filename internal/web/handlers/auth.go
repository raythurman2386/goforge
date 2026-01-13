package handlers

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/user/gocool/internal/auth"
	"github.com/user/gocool/internal/database/repositories"
	"github.com/user/gocool/internal/models"
	"github.com/user/gocool/internal/web/templates/pages"
)

const (
	schemeHTTPS = "https"
)

// AuthHandler handles authentication related requests.
type AuthHandler struct {
	sessionManager *auth.SessionManager
	userRepo       *repositories.UserRepository
	githubProvider *auth.GitHubProvider
	authenticator  auth.Authenticator
}

// NewAuthHandler creates a new AuthHandler.
func NewAuthHandler(sm *auth.SessionManager, ur *repositories.UserRepository, gp *auth.GitHubProvider, a auth.Authenticator) *AuthHandler {
	return &AuthHandler{
		sessionManager: sm,
		userRepo:       ur,
		githubProvider: gp,
		authenticator:  a,
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
	component := pages.Login(csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render login page", http.StatusInternalServerError)
	}
}

// Login handles the login form submission.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	rememberMe := r.FormValue("remember_me") == "on"

	user, err := h.authenticator.Authenticate(r.Context(), email, password)
	if err != nil {
		http.Error(w, "invalid email or password", http.StatusUnauthorized)
		return
	}

	// Create session
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, rememberMe)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, r.URL.Scheme == schemeHTTPS)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// RegisterPage renders the registration page.
func (h *AuthHandler) RegisterPage(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)
	component := pages.Register(csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render register page", http.StatusInternalServerError)
	}
}

// Register handles the registration form submission.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if password != confirmPassword {
		http.Error(w, "passwords do not match", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	_, err := h.userRepo.GetByEmail(r.Context(), email)
	if err == nil {
		http.Error(w, "user already exists", http.StatusBadRequest)
		return
	}

	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	user := &models.User{
		Email:        email,
		PasswordHash: sql.NullString{String: hashedPassword, Valid: true},
	}

	if dbErr := h.userRepo.Create(r.Context(), user); dbErr != nil {
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	// Auto-login after registration
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, false)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, r.URL.Scheme == schemeHTTPS)
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
		MaxAge:   300, // 5 minutes
	})

	url := h.githubProvider.AuthURL(state)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// GitHubCallback handles the GitHub OAuth callback.
func (h *AuthHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("github_oauth_state")
	if err != nil || stateCookie.Value != r.FormValue("state") {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	token, err := h.githubProvider.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to exchange token: %v", err), http.StatusInternalServerError)
		return
	}

	ghUser, err := h.githubProvider.GetUser(r.Context(), token)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get user info: %v", err), http.StatusInternalServerError)
		return
	}

	// Find or create user
	user, err := h.userRepo.GetByGitHubID(r.Context(), ghUser.GetID())
	if err != nil {
		// If not found, create new user
		email := ghUser.GetEmail()
		if email == "" {
			email = fmt.Sprintf("%s@github.com", ghUser.GetLogin())
		}

		user = &models.User{
			Email:          email,
			GitHubID:       sql.NullInt64{Int64: ghUser.GetID(), Valid: true},
			GitHubUsername: sql.NullString{String: ghUser.GetLogin(), Valid: true},
			AvatarURL:      sql.NullString{String: ghUser.GetAvatarURL(), Valid: true},
		}

		// TODO: Encrypt access token before storing
		// user.GitHubAccessToken = sql.NullString{String: token.AccessToken, Valid: true}

		if dbErr := h.userRepo.Create(r.Context(), user); dbErr != nil {
			http.Error(w, fmt.Sprintf("failed to create user: %v", dbErr), http.StatusInternalServerError)
			return
		}
	}

	// Create session
	session, sessionToken, err := h.sessionManager.CreateSession(r.Context(), user.ID, true)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, sessionToken, session.ExpiresAt, r.URL.Scheme == schemeHTTPS)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

// Logout clears the session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := auth.GetSessionToken(r)
	if token != "" {
		if err := h.sessionManager.DestroySession(r.Context(), token); err != nil {
			// Log error but continue with clearing cookie
			fmt.Printf("failed to destroy session: %v\n", err)
		}
	}
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
