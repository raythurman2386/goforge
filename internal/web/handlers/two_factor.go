package handlers

import (
	"encoding/base64"
	"errors"
	"math"
	"net/http"
	"strconv"

	"rsc.io/qr"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/settings"
)

const (
	msgTwoFactorExpired  = "Your sign-in step expired. Enter your email and password again."
	msgTwoFactorTooMany  = "Too many wrong codes. Enter your email and password again."
	msgTwoFactorBadCode  = "Invalid authentication code"
	msgTwoFactorThrottle = "Too many failed sign-in attempts. Try again later."
)

// TwoFactorPage renders the code step after a correct password. Without a
// live challenge cookie it sends the browser back to /login.
func (h *AuthHandler) TwoFactorPage(w http.ResponseWriter, r *http.Request) {
	if r.Context().Value(auth.UserContextKey) != nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	if h.twoFactor == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if _, err := h.twoFactor.Challenge(r.Context(), auth.TwoFactorCookie(r)); err != nil {
		auth.ClearTwoFactorCookie(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.renderTwoFactor(w, r, http.StatusOK, "")
}

// TwoFactorVerify checks the TOTP or recovery code for the pending
// challenge. Wrong codes count against the same login throttle as wrong
// passwords; a right code spends the challenge and creates the session.
func (h *AuthHandler) TwoFactorVerify(w http.ResponseWriter, r *http.Request) {
	if h.twoFactor == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	ch, err := h.twoFactor.Challenge(r.Context(), auth.TwoFactorCookie(r))
	if err != nil {
		if !errors.Is(err, auth.ErrChallengeExpired) {
			h.logger.Error("failed to read two-factor challenge", "error", err)
		}
		auth.ClearTwoFactorCookie(w, r)
		h.renderLogin(w, r, msgTwoFactorExpired)
		return
	}
	user, err := h.userRepo.GetByID(r.Context(), ch.UserID)
	if err != nil {
		auth.ClearTwoFactorCookie(w, r)
		h.renderLogin(w, r, msgTwoFactorExpired)
		return
	}

	clientIP := getClientIP(r)
	if h.loginThrottle != nil {
		if retryAfter, ok := h.loginThrottle.Check(clientIP, user.Email); !ok {
			h.logger.Warn("two-factor step throttled", "client_ip", clientIP)
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			h.renderTwoFactor(w, r, http.StatusTooManyRequests, msgTwoFactorThrottle)
			return
		}
	}

	method, err := h.twoFactor.Verify(r.Context(), user.ID, r.FormValue("code"))
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidTwoFactorCode) && !errors.Is(err, auth.ErrTwoFactorNotEnabled) {
			h.logger.Error("failed to verify two-factor code", "error", err)
			RenderError(w, r, http.StatusInternalServerError, "Sign-in failed. Please try again.")
			return
		}
		if h.loginThrottle != nil {
			h.loginThrottle.Failure(clientIP, user.Email)
		}
		attempts, ended, failErr := h.twoFactor.ChallengeFailed(r.Context(), ch)
		if failErr != nil {
			h.logger.Error("failed to count two-factor attempt", "error", failErr)
		}
		h.record(r, user.ID, models.AuditActionLogin2FAFailed, map[string]interface{}{
			"attempt":         attempts,
			"challenge_ended": ended,
		})
		if ended {
			auth.ClearTwoFactorCookie(w, r)
			h.renderLogin(w, r, msgTwoFactorTooMany)
			return
		}
		h.renderTwoFactor(w, r, http.StatusUnauthorized, msgTwoFactorBadCode)
		return
	}

	if err := h.twoFactor.CompleteChallenge(r.Context(), ch); err != nil {
		auth.ClearTwoFactorCookie(w, r)
		h.renderLogin(w, r, msgTwoFactorExpired)
		return
	}
	if h.loginThrottle != nil {
		h.loginThrottle.Success(user.Email)
	}
	auth.ClearTwoFactorCookie(w, r)
	h.finishLogin(w, r, user, ch.RememberMe, map[string]interface{}{"second_factor": method})
}

// renderLogin sends the user back to the password form with msg (401).
func (h *AuthHandler) renderLogin(w http.ResponseWriter, r *http.Request, msg string) {
	component := pages.Login(auth.GetCSRFToken(r), msg, h.registrationAllowed(r.Context()))
	w.WriteHeader(http.StatusUnauthorized)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render login page", "error", err)
	}
}

func (h *AuthHandler) renderTwoFactor(w http.ResponseWriter, r *http.Request, status int, msg string) {
	component := pages.TwoFactor(auth.GetCSRFToken(r), msg)
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		h.logger.Error("failed to render two-factor page", "error", err)
	}
}

// WithTwoFactor enables the Profile two-factor card and its routes.
func (h *SettingsHandler) WithTwoFactor(tf *auth.TwoFactor) *SettingsHandler {
	h.twoFactor = tf
	return h
}

// twoFactorCard is the Profile card state; nil hides the card.
func (h *SettingsHandler) twoFactorCard(r *http.Request, u *models.User) *settings.TwoFactorCard {
	if h.twoFactor == nil {
		return nil
	}
	c := &settings.TwoFactorCard{PasswordAccount: u.HasPassword(), GitHubLinked: u.IsGitHubUser()}
	if !c.PasswordAccount {
		return c
	}
	st, err := h.twoFactor.Status(r.Context(), u.ID)
	if err != nil {
		if h.logger != nil {
			h.logger.Error("failed to read two-factor status", "error", err)
		}
		return c
	}
	c.Enabled, c.EnabledAt, c.RecoveryCodesLeft = st.Enabled, st.EnabledAt, st.RecoveryCodesLeft
	return c
}

// passwordUser returns the signed-in user fresh from the database after
// checking the password form field. It writes the error response itself.
func (h *SettingsHandler) passwordUser(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return nil, false
	}
	if h.twoFactor == nil {
		RenderError(w, r, http.StatusNotFound, "Two-factor authentication is not available")
		return nil, false
	}
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "invalid form data")
		return nil, false
	}
	dbUser, err := h.userRepo.GetByID(r.Context(), user.ID)
	if err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to load user")
		return nil, false
	}
	if !dbUser.HasPassword() {
		RenderError(w, r, http.StatusBadRequest, "Two-factor authentication is for password sign-in. GitHub sign-in uses your GitHub account's 2FA.")
		return nil, false
	}
	if valid, err := auth.ComparePassword(r.PostFormValue("password"), dbUser.PasswordHash.String); err != nil || !valid {
		RenderError(w, r, http.StatusBadRequest, "Current password is incorrect")
		return nil, false
	}
	return dbUser, true
}

// SetupTwoFactor checks the password, stores a new pending secret, and
// returns the enrollment dialog with the QR code and manual key.
func (h *SettingsHandler) SetupTwoFactor(w http.ResponseWriter, r *http.Request) {
	user, ok := h.passwordUser(w, r)
	if !ok {
		return
	}
	en, err := h.twoFactor.BeginEnrollment(r.Context(), user)
	if errors.Is(err, auth.ErrTwoFactorAlreadyEnabled) {
		RenderError(w, r, http.StatusBadRequest, "Two-factor authentication is already on")
		return
	}
	if err != nil {
		h.logger.Error("failed to start two-factor enrollment", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to start two-factor setup")
		return
	}
	img, err := qrDataURI(en.URI)
	if err != nil {
		h.logger.Error("failed to render two-factor QR code", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to start two-factor setup")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	setup := &settings.TwoFactorSetup{QRDataURI: img, ManualSecret: auth.FormatTOTPSecret(en.Secret)}
	if err := settings.TwoFactorSetupModal(setup, auth.GetCSRFToken(r)).Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to render two-factor setup")
	}
}

// EnableTwoFactor confirms enrollment with the first code and shows the
// recovery codes once.
func (h *SettingsHandler) EnableTwoFactor(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "User not found")
		return
	}
	if h.twoFactor == nil {
		RenderError(w, r, http.StatusNotFound, "Two-factor authentication is not available")
		return
	}
	if err := r.ParseForm(); err != nil {
		RenderError(w, r, http.StatusBadRequest, "invalid form data")
		return
	}
	codes, err := h.twoFactor.ConfirmEnrollment(r.Context(), user.ID, r.PostFormValue("code"))
	switch {
	case errors.Is(err, auth.ErrInvalidTwoFactorCode):
		RenderError(w, r, http.StatusBadRequest, "That code did not match. Check the time on your device and try again.")
		return
	case errors.Is(err, auth.ErrNoPendingEnrollment):
		RenderError(w, r, http.StatusBadRequest, "Start two-factor setup again from your profile")
		return
	case errors.Is(err, auth.ErrTwoFactorAlreadyEnabled):
		RenderError(w, r, http.StatusBadRequest, "Two-factor authentication is already on")
		return
	case err != nil:
		h.logger.Error("failed to confirm two-factor enrollment", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to turn on two-factor authentication")
		return
	}
	h.recordUser(r, user.ID, models.AuditActionUser2FAEnable, map[string]interface{}{"recovery_codes": len(codes)})
	w.Header().Set("Cache-Control", "no-store")
	if err := settings.RecoveryCodesModal(codes).Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to render recovery codes")
	}
}

// DisableTwoFactor turns 2FA off after the password and a current TOTP or
// recovery code.
func (h *SettingsHandler) DisableTwoFactor(w http.ResponseWriter, r *http.Request) {
	user, ok := h.passwordUser(w, r)
	if !ok {
		return
	}
	method, err := h.twoFactor.Disable(r.Context(), user.ID, r.PostFormValue("code"))
	switch {
	case errors.Is(err, auth.ErrInvalidTwoFactorCode):
		RenderError(w, r, http.StatusBadRequest, "Invalid authentication code")
		return
	case errors.Is(err, auth.ErrTwoFactorNotEnabled):
		RenderError(w, r, http.StatusBadRequest, "Two-factor authentication is not on")
		return
	case err != nil:
		h.logger.Error("failed to turn off two-factor authentication", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Failed to turn off two-factor authentication")
		return
	}
	h.recordUser(r, user.ID, models.AuditActionUser2FADisable, map[string]interface{}{"second_factor": method})
	w.Header().Set("HX-Refresh", "true")
	if err := components.Toast("Two-factor authentication turned off", "success").Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "Failed to render response")
	}
}

// qrDataURI renders text as a QR code PNG data: URI (no third-party
// request, so the TOTP secret never leaves the server).
func qrDataURI(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	code.Scale = 5
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}
