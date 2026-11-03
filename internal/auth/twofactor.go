package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Sign-in challenge limits. A challenge is the pending step after a correct
// password for an account with 2FA on; it is not a session.
const (
	LoginChallengeTTL         = 5 * time.Minute
	MaxLoginChallengeAttempts = 5
	// TwoFactorCookieName holds the challenge token between /auth/login and
	// /auth/2fa. It is HttpOnly, SameSite=Lax, and lives LoginChallengeTTL.
	TwoFactorCookieName = "goforge_2fa"
)

var (
	// ErrInvalidTwoFactorCode is a wrong, expired, or already used TOTP or
	// recovery code.
	ErrInvalidTwoFactorCode = errors.New("invalid two-factor code")
	// ErrChallengeExpired means the sign-in challenge is missing, expired,
	// used, or ended after too many wrong codes.
	ErrChallengeExpired = errors.New("sign-in challenge expired")
	// ErrTwoFactorNotEnabled means the account has no confirmed enrollment.
	ErrTwoFactorNotEnabled = errors.New("two-factor authentication is not enabled")
	// ErrTwoFactorAlreadyEnabled means enrollment was asked for while 2FA is on.
	ErrTwoFactorAlreadyEnabled = errors.New("two-factor authentication is already enabled")
	// ErrNoPendingEnrollment means a confirm came without a started enrollment.
	ErrNoPendingEnrollment = errors.New("no two-factor enrollment in progress")
)

// TwoFactorStore is the storage TwoFactor needs. *repositories.TwoFactorRepository
// implements it; tests use an in-memory fake.
type TwoFactorStore interface {
	GetTOTP(ctx context.Context, userID string) (*models.UserTOTP, error)
	SavePendingTOTP(ctx context.Context, userID, secret string) error
	EnableTOTP(ctx context.Context, userID string, step int64, codeHashes []string) error
	UseTOTPStep(ctx context.Context, userID string, step int64) (bool, error)
	UseRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error)
	CountUnusedRecoveryCodes(ctx context.Context, userID string) (int, error)
	DeleteTOTP(ctx context.Context, userID string) (bool, error)
	CreateChallenge(ctx context.Context, c *models.LoginChallenge) error
	GetChallengeByTokenHash(ctx context.Context, tokenHash string) (*models.LoginChallenge, error)
	IncrementChallengeAttempts(ctx context.Context, id string) (int, error)
	DeleteChallenge(ctx context.Context, id string) (bool, error)
}

// TwoFactor runs opt-in TOTP for local password accounts: enrollment,
// verification with replay protection, one-time recovery codes, and the
// short-lived sign-in challenge. GitHub OAuth sign-in never calls it.
type TwoFactor struct {
	store TwoFactorStore
	now   func() time.Time
}

// NewTwoFactor creates a TwoFactor over store.
func NewTwoFactor(store TwoFactorStore) *TwoFactor {
	return &TwoFactor{store: store, now: time.Now}
}

// WithClock replaces the clock (tests).
func (t *TwoFactor) WithClock(now func() time.Time) *TwoFactor {
	t.now = now
	return t
}

// TwoFactorStatus is what Settings → Profile shows.
type TwoFactorStatus struct {
	Enabled           bool
	EnabledAt         time.Time
	RecoveryCodesLeft int
}

// Enrollment is a started, unconfirmed enrollment. Secret is shown once
// for manual entry; URI is what the QR code encodes.
type Enrollment struct {
	Secret string
	URI    string
}

// Enabled reports whether userID has confirmed 2FA. A stored secret that no
// longer decrypts still counts as enabled, so sign-in fails closed.
func (t *TwoFactor) Enabled(ctx context.Context, userID string) (bool, error) {
	rec, err := t.store.GetTOTP(ctx, userID)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return false, nil
	case errors.Is(err, repositories.ErrTwoFactorUnreadable):
		return rec.Enabled(), nil
	case err != nil:
		return false, err
	}
	return rec.Enabled(), nil
}

// Status returns the account's 2FA state for display.
func (t *TwoFactor) Status(ctx context.Context, userID string) (TwoFactorStatus, error) {
	rec, err := t.store.GetTOTP(ctx, userID)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return TwoFactorStatus{}, nil
	case err != nil && !errors.Is(err, repositories.ErrTwoFactorUnreadable):
		return TwoFactorStatus{}, err
	}
	if !rec.Enabled() {
		return TwoFactorStatus{}, nil
	}
	left, err := t.store.CountUnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return TwoFactorStatus{}, err
	}
	return TwoFactorStatus{Enabled: true, EnabledAt: rec.EnabledAt.Time, RecoveryCodesLeft: left}, nil
}

// BeginEnrollment stores a fresh pending secret for user and returns it.
// Starting again replaces an unconfirmed secret.
func (t *TwoFactor) BeginEnrollment(ctx context.Context, user *models.User) (*Enrollment, error) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return nil, err
	}
	if err := t.store.SavePendingTOTP(ctx, user.ID, secret); err != nil {
		if errors.Is(err, repositories.ErrAlreadyExists) {
			return nil, ErrTwoFactorAlreadyEnabled
		}
		return nil, err
	}
	return &Enrollment{Secret: secret, URI: OTPAuthURI(user.Email, secret)}, nil
}

// ConfirmEnrollment turns 2FA on when code matches the pending secret and
// returns the new recovery codes in plaintext. They are stored hashed and
// cannot be shown again.
func (t *TwoFactor) ConfirmEnrollment(ctx context.Context, userID, code string) ([]string, error) {
	rec, err := t.store.GetTOTP(ctx, userID)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return nil, ErrNoPendingEnrollment
	case err != nil:
		return nil, err
	}
	if rec.Enabled() {
		return nil, ErrTwoFactorAlreadyEnabled
	}
	step, ok := VerifyTOTP(rec.Secret, normalizeTOTPInput(code), t.now(), rec.LastUsedStep)
	if !ok {
		return nil, ErrInvalidTwoFactorCode
	}
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, err
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = HashRecoveryCode(c)
	}
	if err := t.store.EnableTOTP(ctx, userID, step, hashes); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, ErrNoPendingEnrollment
		}
		return nil, err
	}
	return codes, nil
}

// Verify checks a second factor for an account with 2FA on. A 6-digit input
// is a TOTP code (±1 step, each step once); anything else is a recovery
// code (each code once). It returns models.SecondFactorTOTP or
// models.SecondFactorRecoveryCode.
func (t *TwoFactor) Verify(ctx context.Context, userID, input string) (string, error) {
	rec, err := t.store.GetTOTP(ctx, userID)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return "", ErrTwoFactorNotEnabled
	case errors.Is(err, repositories.ErrTwoFactorUnreadable):
		if !rec.Enabled() {
			return "", ErrTwoFactorNotEnabled
		}
		// The secret cannot be checked; recovery codes still can.
	case err != nil:
		return "", err
	}
	if !rec.Enabled() {
		return "", ErrTwoFactorNotEnabled
	}
	if IsTOTPCodeShape(input) {
		if rec.Secret == "" {
			return "", ErrInvalidTwoFactorCode
		}
		step, ok := VerifyTOTP(rec.Secret, normalizeTOTPInput(input), t.now(), rec.LastUsedStep)
		if !ok {
			return "", ErrInvalidTwoFactorCode
		}
		used, useErr := t.store.UseTOTPStep(ctx, userID, step)
		if useErr != nil {
			return "", useErr
		}
		if !used {
			return "", ErrInvalidTwoFactorCode
		}
		return models.SecondFactorTOTP, nil
	}
	if NormalizeRecoveryCode(input) == "" {
		return "", ErrInvalidTwoFactorCode
	}
	used, err := t.store.UseRecoveryCode(ctx, userID, HashRecoveryCode(input))
	if err != nil {
		return "", err
	}
	if !used {
		return "", ErrInvalidTwoFactorCode
	}
	return models.SecondFactorRecoveryCode, nil
}

// Disable verifies a current code and then removes the enrollment, recovery
// codes, and pending challenges. The caller checks the password first.
func (t *TwoFactor) Disable(ctx context.Context, userID, input string) (string, error) {
	method, err := t.Verify(ctx, userID, input)
	if err != nil {
		return "", err
	}
	if _, err := t.store.DeleteTOTP(ctx, userID); err != nil {
		return "", err
	}
	return method, nil
}

// Reset removes 2FA with no code (the `goforge user reset-2fa` escape
// hatch). It reports whether anything was removed.
func (t *TwoFactor) Reset(ctx context.Context, userID string) (bool, error) {
	return t.store.DeleteTOTP(ctx, userID)
}

// StartChallenge creates the pending sign-in step for userID after a correct
// password and returns the random token for the goforge_2fa cookie. Only
// the token's hash is stored.
func (t *TwoFactor) StartChallenge(ctx context.Context, userID string, rememberMe bool) (string, *models.LoginChallenge, error) {
	token, err := generateRandomToken(32)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate challenge token: %w", err)
	}
	now := t.now()
	c := &models.LoginChallenge{
		TokenHash:  hashToken(token),
		UserID:     userID,
		RememberMe: rememberMe,
		ExpiresAt:  now.Add(LoginChallengeTTL),
		CreatedAt:  now,
	}
	if err := t.store.CreateChallenge(ctx, c); err != nil {
		return "", nil, err
	}
	return token, c, nil
}

// Challenge returns the live challenge for token. A missing, expired, or
// spent challenge returns ErrChallengeExpired; an expired one is deleted.
func (t *TwoFactor) Challenge(ctx context.Context, token string) (*models.LoginChallenge, error) {
	if token == "" {
		return nil, ErrChallengeExpired
	}
	c, err := t.store.GetChallengeByTokenHash(ctx, hashToken(token))
	if errors.Is(err, repositories.ErrNotFound) {
		return nil, ErrChallengeExpired
	}
	if err != nil {
		return nil, err
	}
	if c.Expired(t.now()) || c.Attempts >= MaxLoginChallengeAttempts {
		_, _ = t.store.DeleteChallenge(ctx, c.ID) //nolint:errcheck // best effort: the challenge is refused either way
		return nil, ErrChallengeExpired
	}
	return c, nil
}

// ChallengeFailed counts a wrong code against c. After
// MaxLoginChallengeAttempts the challenge is deleted and ended is true, so
// the user has to enter the password again.
func (t *TwoFactor) ChallengeFailed(ctx context.Context, c *models.LoginChallenge) (attempts int, ended bool, err error) {
	attempts, err = t.store.IncrementChallengeAttempts(ctx, c.ID)
	if errors.Is(err, repositories.ErrNotFound) {
		return c.Attempts + 1, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	if attempts >= MaxLoginChallengeAttempts {
		_, _ = t.store.DeleteChallenge(ctx, c.ID) //nolint:errcheck // best effort: the challenge is refused either way
		return attempts, true, nil
	}
	return attempts, false, nil
}

// CompleteChallenge spends c before a session is created. It returns
// ErrChallengeExpired when another request already spent it.
func (t *TwoFactor) CompleteChallenge(ctx context.Context, c *models.LoginChallenge) error {
	ok, err := t.store.DeleteChallenge(ctx, c.ID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrChallengeExpired
	}
	return nil
}

// SetTwoFactorCookie stores the challenge token in the browser.
func SetTwoFactorCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     TwoFactorCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(LoginChallengeTTL / time.Second),
		HttpOnly: true,
		Secure:   IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearTwoFactorCookie removes the challenge cookie.
func ClearTwoFactorCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     TwoFactorCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// TwoFactorCookie returns the challenge token from r, or "".
func TwoFactorCookie(r *http.Request) string {
	c, err := r.Cookie(TwoFactorCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func normalizeTOTPInput(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '-' {
			out = append(out, r)
		}
	}
	return string(out)
}
