package handlers

import (
	"context"
	"database/sql"
	"encoding/binary"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
	"github.com/raythurman2386/goforge/internal/web/httputil"
	"github.com/raythurman2386/goforge/internal/web/middleware"
)

const tfPassword = "two-factor-pass-123"

type tfClock struct{ t time.Time }

func (c *tfClock) now() time.Time          { return c.t }
func (c *tfClock) advance(d time.Duration) { c.t = c.t.Add(d) }

type tfEnv struct {
	db       *database.DB
	users    *repositories.UserRepository
	repo     *repositories.TwoFactorRepository
	manager  *auth.SessionManager
	tf       *auth.TwoFactor
	clk      *tfClock
	settings *SettingsHandler
	authH    *AuthHandler
	setAudit *fakeUIAudit
	logAudit *fakeUIAudit
	throttle *middleware.LoginThrottle
	logger   *slog.Logger
}

// newTFEnv wires real repositories against DATABASE_URL (skips without a
// database) and a fake clock so TOTP steps can move forward.
func newTFEnv(t *testing.T) *tfEnv {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("skipping; no database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.MigrateUp(db, cfg); err != nil {
		t.Skipf("skipping; migrate: %v", err)
	}
	SetProxyTrust(httputil.NewProxyTrust(nil, false))
	t.Cleanup(func() { SetProxyTrust(httputil.ProxyTrust{}) })

	key, err := secrets.GenerateKeyString(32)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := secrets.NewEncryptorFromString(key)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := &tfEnv{db: db, logger: logger, clk: &tfClock{t: time.Now()}}
	e.users = repositories.NewUserRepository(db.DB, enc)
	e.repo = repositories.NewTwoFactorRepository(db.DB, enc)
	e.manager = auth.NewSessionManager(repositories.NewSessionRepository(db.DB), e.users, logger)
	e.tf = auth.NewTwoFactor(e.repo).WithClock(e.clk.now)
	e.setAudit, e.logAudit = &fakeUIAudit{}, &fakeUIAudit{}
	e.throttle = middleware.NewLoginThrottle(1000, 1000, 15*time.Minute, 15*time.Minute)
	t.Cleanup(e.throttle.Stop)
	e.settings = NewSettingsHandler(nil, nil, e.users, nil, logger).WithAudit(e.setAudit).WithTwoFactor(e.tf)
	e.authH = NewAuthHandler(e.manager, e.users, nil, auth.NewLocalAuthenticator(e.users), nil, false, logger).
		WithAuditRecorder(e.logAudit).WithLoginThrottle(e.throttle).WithTwoFactor(e.tf)
	return e
}

func (e *tfEnv) newUser(t *testing.T, ghID int64) *models.User {
	t.Helper()
	hash, err := auth.HashPassword(tfPassword)
	if err != nil {
		t.Fatal(err)
	}
	u := &models.User{Email: "tf-" + uuid.NewString() + "@example.com", PasswordHash: sql.NullString{String: hash, Valid: true}}
	if ghID != 0 {
		u.GitHubID = sql.NullInt64{Int64: ghID, Valid: true}
		u.GitHubUsername = sql.NullString{String: "tf-gh", Valid: true}
	}
	if err := e.users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func (e *tfEnv) settingsPost(u *models.User, fn func(http.ResponseWriter, *http.Request), path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()
	fn(rr, req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, u)))
	return rr
}

func (e *tfEnv) login(email string) *httptest.ResponseRecorder {
	form := url.Values{"email": {email}, "password": {tfPassword}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.20:4000"
	rr := httptest.NewRecorder()
	e.authH.Login(rr, req)
	return rr
}

func (e *tfEnv) secondStep(cookie *http.Cookie, code string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/2fa", strings.NewReader(url.Values{"code": {code}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.20:4000"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	e.authH.TwoFactorVerify(rr, req)
	return rr
}

func cookieNamed(rr *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func (e *tfEnv) code(t *testing.T, secret string) string {
	t.Helper()
	c, err := auth.TOTPCode(secret, auth.TOTPStep(e.clk.now()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *tfEnv) sessionCount(t *testing.T, userID string) int {
	t.Helper()
	var n int
	if err := e.db.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var (
	secretRE = regexp.MustCompile(`id="two-factor-secret"[^>]*>([^<]+)<`)
	codesRE  = regexp.MustCompile(`id="recovery-codes"[^>]*>([^<]+)<`)
)

// enroll drives Settings → Security setup and confirm, returning the TOTP
// secret and the recovery codes the dialogs showed.
func (e *tfEnv) enroll(t *testing.T, u *models.User) (string, []string) {
	t.Helper()
	rr := e.settingsPost(u, e.settings.SetupTwoFactor, "/settings/2fa/setup", url.Values{"password": {tfPassword}})
	if rr.Code != http.StatusOK {
		t.Fatalf("setup: status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `src="data:image/png;base64,`) {
		t.Fatal("setup dialog has no inline QR image")
	}
	m := secretRE.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("setup dialog has no manual secret")
	}
	secret := strings.ReplaceAll(html.UnescapeString(m[1]), " ", "")

	rr = e.settingsPost(u, e.settings.EnableTwoFactor, "/settings/2fa/enable", url.Values{"code": {e.code(t, secret)}})
	if rr.Code != http.StatusOK {
		t.Fatalf("enable: status=%d body=%s", rr.Code, rr.Body.String())
	}
	cm := codesRE.FindStringSubmatch(rr.Body.String())
	if cm == nil {
		t.Fatal("recovery codes dialog has no codes")
	}
	codes := strings.Fields(cm[1])
	if len(codes) != auth.RecoveryCodeCount {
		t.Fatalf("got %d recovery codes", len(codes))
	}
	return secret, codes
}

func TestTwoFactor_EnrollLoginDisable(t *testing.T) {
	e := newTFEnv(t)
	u := e.newUser(t, 0)

	// Without 2FA, password sign-in goes straight to the dashboard.
	if rr := e.login(u.Email); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/dashboard" {
		t.Fatalf("login before enroll: %d %s", rr.Code, rr.Header().Get("Location"))
	}

	// Setup needs the current password.
	if rr := e.settingsPost(u, e.settings.SetupTwoFactor, "/settings/2fa/setup", url.Values{"password": {"wrong"}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("setup with wrong password: %d", rr.Code)
	}
	// A wrong first code does not turn 2FA on.
	if rr := e.settingsPost(u, e.settings.SetupTwoFactor, "/settings/2fa/setup", url.Values{"password": {tfPassword}}); rr.Code != http.StatusOK {
		t.Fatalf("setup: %d", rr.Code)
	}
	if rr := e.settingsPost(u, e.settings.EnableTwoFactor, "/settings/2fa/enable", url.Values{"code": {"000000"}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("enable with wrong code: %d", rr.Code)
	}
	if n := len(e.setAudit.snapshot()); n != 0 {
		t.Fatalf("failed setup wrote %d audit rows", n)
	}

	secret, codes := e.enroll(t, u)
	rows := e.setAudit.snapshot()
	if len(rows) != 1 || rows[0].action != models.AuditActionUser2FAEnable || rows[0].details["recovery_codes"] != auth.RecoveryCodeCount || rows[0].details["via"] != models.AuditViaUI {
		t.Fatalf("enable audit rows = %+v", rows)
	}
	// The secret is sealed at rest.
	var sealed string
	if err := e.db.DB.QueryRow(`SELECT secret_sealed FROM user_totp WHERE user_id = $1`, u.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, secret) {
		t.Fatal("TOTP secret stored in plaintext")
	}
	// Recovery codes are stored hashed.
	var plain int
	if err := e.db.DB.QueryRow(`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1 AND code_hash = $2`, u.ID, codes[0]).Scan(&plain); err != nil || plain != 0 {
		t.Fatalf("recovery code stored in plaintext (n=%d, err=%v)", plain, err)
	}

	before := e.sessionCount(t, u.ID)

	// Correct password now stops at the second step: no session yet.
	pw := e.login(u.Email)
	if pw.Code != http.StatusSeeOther || pw.Header().Get("Location") != "/auth/2fa" {
		t.Fatalf("login with 2FA: %d %s", pw.Code, pw.Header().Get("Location"))
	}
	if cookieNamed(pw, auth.SessionCookieName) != nil {
		t.Fatal("password step set a session cookie")
	}
	ch := cookieNamed(pw, auth.TwoFactorCookieName)
	if ch == nil || !ch.HttpOnly || ch.MaxAge != int(auth.LoginChallengeTTL/time.Second) {
		t.Fatalf("challenge cookie = %+v", ch)
	}
	if n := e.sessionCount(t, u.ID); n != before {
		t.Fatalf("password step created a session (%d -> %d)", before, n)
	}

	// No cookie, and no code: refused.
	if rr := e.secondStep(nil, e.code(t, secret)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("second step without challenge: %d", rr.Code)
	}
	if rr := e.secondStep(ch, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("second step without code: %d", rr.Code)
	}
	// The enrollment code's step is spent; a replay is refused.
	if rr := e.secondStep(ch, e.code(t, secret)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("replayed enrollment code: %d", rr.Code)
	}
	fails := 0
	for _, r := range e.logAudit.snapshot() {
		if r.action == models.AuditActionLogin2FAFailed {
			fails++
			if _, ok := r.details["attempt"]; !ok {
				t.Errorf("login.2fa_failed without attempt: %+v", r.details)
			}
			if r.details["code"] != nil {
				t.Error("login.2fa_failed stored the code")
			}
		}
	}
	if fails != 2 {
		t.Fatalf("login.2fa_failed rows = %d, want 2", fails)
	}

	e.clk.advance(auth.TOTPPeriod)
	ok := e.secondStep(ch, e.code(t, secret))
	if ok.Code != http.StatusSeeOther || ok.Header().Get("Location") != "/dashboard" {
		t.Fatalf("TOTP second step: %d %s", ok.Code, ok.Header().Get("Location"))
	}
	if cookieNamed(ok, auth.SessionCookieName) == nil {
		t.Fatal("no session cookie after TOTP")
	}
	if c := cookieNamed(ok, auth.TwoFactorCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatal("challenge cookie not cleared")
	}
	rows = e.logAudit.snapshot()
	last := rows[len(rows)-1]
	if last.action != models.AuditActionLogin || last.details["second_factor"] != models.SecondFactorTOTP {
		t.Fatalf("login audit row = %+v", last)
	}
	// The challenge is spent.
	e.clk.advance(auth.TOTPPeriod)
	if rr := e.secondStep(ch, e.code(t, secret)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("reused challenge: %d", rr.Code)
	}

	// A recovery code works once.
	ch = cookieNamed(e.login(u.Email), auth.TwoFactorCookieName)
	if rr := e.secondStep(ch, strings.ToUpper(codes[0])); rr.Code != http.StatusSeeOther {
		t.Fatalf("recovery code: %d", rr.Code)
	}
	rows = e.logAudit.snapshot()
	if d := rows[len(rows)-1].details["second_factor"]; d != models.SecondFactorRecoveryCode {
		t.Fatalf("second_factor = %v", d)
	}
	ch = cookieNamed(e.login(u.Email), auth.TwoFactorCookieName)
	if rr := e.secondStep(ch, codes[0]); rr.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code: %d", rr.Code)
	}

	// Disable needs the password and a current code.
	e.clk.advance(auth.TOTPPeriod)
	if rr := e.settingsPost(u, e.settings.DisableTwoFactor, "/settings/2fa/disable", url.Values{"password": {"wrong"}, "code": {e.code(t, secret)}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("disable with wrong password: %d", rr.Code)
	}
	if rr := e.settingsPost(u, e.settings.DisableTwoFactor, "/settings/2fa/disable", url.Values{"password": {tfPassword}, "code": {"000000"}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("disable with wrong code: %d", rr.Code)
	}
	off := e.settingsPost(u, e.settings.DisableTwoFactor, "/settings/2fa/disable", url.Values{"password": {tfPassword}, "code": {e.code(t, secret)}})
	if off.Code != http.StatusOK || off.Header().Get("HX-Refresh") != htmxTrue {
		t.Fatalf("disable: %d %s", off.Code, off.Body.String())
	}
	rows = e.setAudit.snapshot()
	last = rows[len(rows)-1]
	if last.action != models.AuditActionUser2FADisable || last.details["second_factor"] != models.SecondFactorTOTP || last.details["via"] != models.AuditViaUI {
		t.Fatalf("disable audit row = %+v", last)
	}
	var left int
	if err := e.db.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM user_totp WHERE user_id = $1) + (SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1) + (SELECT COUNT(*) FROM login_challenges WHERE user_id = $1)`, u.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left after disable = %d (%v)", left, err)
	}
	// Password sign-in is one step again.
	if rr := e.login(u.Email); rr.Header().Get("Location") != "/dashboard" {
		t.Fatalf("login after disable: %d %s", rr.Code, rr.Header().Get("Location"))
	}
}

// The pending step expires after LoginChallengeTTL; GET /auth/2fa then sends
// the browser back to /login and POST refuses the code.
func TestTwoFactor_ChallengeExpires(t *testing.T) {
	e := newTFEnv(t)
	u := e.newUser(t, 0)
	secret, _ := e.enroll(t, u)
	ch := cookieNamed(e.login(u.Email), auth.TwoFactorCookieName)

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/2fa", nil)
		req.AddCookie(ch)
		rr := httptest.NewRecorder()
		e.authH.TwoFactorPage(rr, req)
		return rr
	}
	if rr := get(); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `name="code"`) {
		t.Fatalf("2fa page: %d", rr.Code)
	}
	e.clk.advance(auth.LoginChallengeTTL + time.Second)
	if rr := get(); rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login" {
		t.Fatalf("expired 2fa page: %d %s", rr.Code, rr.Header().Get("Location"))
	}
	if rr := e.secondStep(ch, e.code(t, secret)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("code on expired challenge: %d", rr.Code)
	}
}

// Wrong codes count against the same login throttle as wrong passwords and
// end in 429; a challenge also ends after MaxLoginChallengeAttempts.
func TestTwoFactor_WrongCodesHitThrottle(t *testing.T) {
	e := newTFEnv(t)
	u := e.newUser(t, 0)
	secret, _ := e.enroll(t, u)
	lt := middleware.NewLoginThrottle(1000, 3, 15*time.Minute, 15*time.Minute)
	t.Cleanup(lt.Stop)
	e.authH.WithLoginThrottle(lt)

	ch := cookieNamed(e.login(u.Email), auth.TwoFactorCookieName)
	for i := 0; i < 3; i++ {
		if rr := e.secondStep(ch, "000000"); rr.Code != http.StatusUnauthorized {
			t.Fatalf("wrong code %d: %d", i+1, rr.Code)
		}
	}
	e.clk.advance(auth.TOTPPeriod)
	rr := e.secondStep(ch, e.code(t, secret))
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("right code while locked: %d", rr.Code)
	}
	if cookieNamed(rr, auth.SessionCookieName) != nil {
		t.Fatal("locked step set a session")
	}
	// The password step is locked for the account too.
	if rr := e.login(u.Email); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("password while locked: %d", rr.Code)
	}

	// Per-challenge cap with a roomy throttle.
	e.authH.WithLoginThrottle(e.throttle)
	ch = cookieNamed(e.login(u.Email), auth.TwoFactorCookieName)
	for i := 1; i <= auth.MaxLoginChallengeAttempts; i++ {
		rr := e.secondStep(ch, "000000")
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rr.Code)
		}
		if i == auth.MaxLoginChallengeAttempts && !strings.Contains(rr.Body.String(), "Too many wrong codes") {
			t.Fatal("challenge did not end at the cap")
		}
	}
	e.clk.advance(auth.TOTPPeriod)
	if rr := e.secondStep(ch, e.code(t, secret)); rr.Code != http.StatusUnauthorized {
		t.Fatalf("right code on ended challenge: %d", rr.Code)
	}
}

type fakeGitHub struct{ user *github.User }

func (f *fakeGitHub) AuthURL(state string) string {
	return "https://github.test/authorize?state=" + state
}
func (f *fakeGitHub) Exchange(context.Context, string) (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "fake-oauth-value"}, nil
}
func (f *fakeGitHub) GetUser(context.Context, *oauth2.Token) (*github.User, error) {
	return f.user, nil
}

// GitHub OAuth sign-in relies on GitHub's own 2FA: even for an account with
// GoForge TOTP on, the callback creates the session with no TOTP step.
func TestTwoFactor_GitHubCallbackSkipsTOTP(t *testing.T) {
	e := newTFEnv(t)
	ghID := tfGitHubID(1 << 41)
	u := e.newUser(t, ghID)
	e.enroll(t, u)
	if on, err := e.tf.Enabled(context.Background(), u.ID); err != nil || !on {
		t.Fatalf("2FA not on: %v %v", on, err)
	}
	e.authH.WithGitHubProvider(&fakeGitHub{user: &github.User{ID: github.Int64(ghID), Login: github.String("tf-gh"), Email: github.String(u.Email)}})

	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state=s1&code=c1", nil)
	req.AddCookie(&http.Cookie{Name: "github_oauth_state", Value: "s1"})
	rr := httptest.NewRecorder()
	e.authH.GitHubCallback(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/dashboard" {
		t.Fatalf("callback: %d %s %s", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
	if cookieNamed(rr, auth.SessionCookieName) == nil {
		t.Fatal("callback set no session cookie")
	}
	if cookieNamed(rr, auth.TwoFactorCookieName) != nil {
		t.Fatal("callback started a TOTP step")
	}
	var challenges int
	if err := e.db.DB.QueryRow(`SELECT COUNT(*) FROM login_challenges WHERE user_id = $1`, u.ID).Scan(&challenges); err != nil || challenges != 0 {
		t.Fatalf("challenges = %d (%v)", challenges, err)
	}
	rows := e.logAudit.snapshot()
	last := rows[len(rows)-1]
	if last.action != models.AuditActionLogin || last.details["provider"] != "github" || last.details["second_factor"] != nil {
		t.Fatalf("github login audit row = %+v", last)
	}
}

// GitHub-only accounts get a note instead of the enrollment form, and the
// setup route refuses them.
func TestTwoFactor_GitHubOnlyAccountCannotEnroll(t *testing.T) {
	e := newTFEnv(t)
	u := &models.User{Email: "tf-gh-" + uuid.NewString() + "@example.com", GitHubID: sql.NullInt64{Int64: tfGitHubID(1 << 42), Valid: true}}
	if err := e.users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if rr := e.settingsPost(u, e.settings.SetupTwoFactor, "/settings/2fa/setup", url.Values{"password": {""}}); rr.Code != http.StatusBadRequest {
		t.Fatalf("github-only setup: %d", rr.Code)
	}
	card := e.settings.twoFactorCard(httptest.NewRequest(http.MethodGet, "/", nil), u)
	if card == nil || card.PasswordAccount {
		t.Fatalf("card = %+v", card)
	}
}

// tfGitHubID is a random GitHub user ID above base, unique per test run.
func tfGitHubID(base int64) int64 {
	id := uuid.New()
	return base + int64(binary.BigEndian.Uint32(id[:4]))
}
