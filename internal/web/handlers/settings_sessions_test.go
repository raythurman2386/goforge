package handlers

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// A successful password change deletes every other session of the account,
// keeps the one that made the change, leaves other accounts alone, and
// writes user.password.change with sessions_revoked. Uses DATABASE_URL
// (skips when no database is reachable), like the repository tests.
func TestSettingsHandler_UpdatePassword_RevokesOtherSessions(t *testing.T) {
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

	ctx := context.Background()
	users := repositories.NewUserRepository(db.DB, nil)
	sessions := repositories.NewSessionRepository(db.DB)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := auth.NewSessionManager(sessions, users, logger)

	const oldPassword = "old-password-123"
	hash, err := auth.HashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{Email: "pw-revoke-" + uuid.NewString() + "@example.com", PasswordHash: sql.NullString{String: hash, Valid: true}}
	other := &models.User{Email: "pw-revoke-other-" + uuid.NewString() + "@example.com"}
	for _, u := range []*models.User{user, other} {
		if err = users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	sessA, tokenA, err := manager.CreateSession(ctx, user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, err := manager.CreateSession(ctx, user.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	_, tokenOther, err := manager.CreateSession(ctx, other.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	rec := &fakeUIAudit{}
	h := NewSettingsHandler(nil, nil, users, nil, logger).WithAudit(rec).WithSessions(sessions)

	put := func(current, next string) *httptest.ResponseRecorder {
		form := url.Values{"current_password": {current}, "new_password": {next}, "confirm_password": {next}}
		req := httptest.NewRequest(http.MethodPut, "/settings/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c := context.WithValue(req.Context(), auth.UserContextKey, user)
		c = context.WithValue(c, auth.SessionContextKey, sessA)
		rr := httptest.NewRecorder()
		h.UpdatePassword(rr, req.WithContext(c))
		return rr
	}

	// A wrong current password changes nothing and revokes nothing.
	if rr := put("not-the-password", "new-password-456"); rr.Code != http.StatusBadRequest {
		t.Fatalf("wrong current password: status=%d", rr.Code)
	}
	if _, _, err = manager.GetSession(ctx, tokenB); err != nil {
		t.Fatalf("session B revoked by a failed change: %v", err)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("failed change wrote %d audit rows", n)
	}

	rr := put(oldPassword, "new-password-456")
	if rr.Code != http.StatusOK {
		t.Fatalf("change: status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Signed out 1 other session.") {
		t.Errorf("toast = %q", rr.Body.String())
	}

	if _, _, err = manager.GetSession(ctx, tokenA); err != nil {
		t.Errorf("current session A revoked: %v", err)
	}
	if _, _, err = manager.GetSession(ctx, tokenB); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("session B still valid (err=%v)", err)
	}
	if _, _, err = manager.GetSession(ctx, tokenOther); err != nil {
		t.Errorf("another account's session revoked: %v", err)
	}

	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %+v", rows)
	}
	row := rows[0]
	if row.action != models.AuditActionUserPasswordChange || row.userID != user.ID ||
		row.resourceType != models.ResourceTypeUser || row.resourceID != user.ID {
		t.Errorf("audit row = %+v", row)
	}
	if row.details["sessions_revoked"] != int64(1) || row.details["via"] != models.AuditViaUI {
		t.Errorf("audit details = %#v", row.details)
	}
	for k := range row.details {
		if strings.Contains(strings.ToLower(k), "password") || strings.Contains(strings.ToLower(k), "hash") {
			t.Errorf("audit details carry %q", k)
		}
	}

	dbUser, err := users.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := auth.ComparePassword("new-password-456", dbUser.PasswordHash.String); !ok {
		t.Error("new password not stored")
	}
}

// fakeRevoker records DeleteByUserIDExcept calls.
type fakeRevoker struct {
	userID, keep string
	n            int64
	err          error
	calls        int
}

func (f *fakeRevoker) DeleteByUserIDExcept(_ context.Context, userID, keep string) (int64, error) {
	f.calls++
	f.userID, f.keep = userID, keep
	return f.n, f.err
}

// "Sign out other sessions" keeps the request's session and writes
// user.sessions.revoke with the count.
func TestSettingsHandler_RevokeOtherSessions(t *testing.T) {
	user := &models.User{ID: uuid.NewString(), Email: "revoke@example.com"}
	sess := &models.Session{ID: uuid.NewString(), UserID: user.ID}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	call := func(h *SettingsHandler, withSession bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/settings/sessions/revoke-others", nil)
		c := context.WithValue(req.Context(), auth.UserContextKey, user)
		if withSession {
			c = context.WithValue(c, auth.SessionContextKey, sess)
		}
		rr := httptest.NewRecorder()
		h.RevokeOtherSessions(rr, req.WithContext(c))
		return rr
	}

	t.Run("keeps current session", func(t *testing.T) {
		rev := &fakeRevoker{n: 3}
		rec := &fakeUIAudit{}
		h := NewSettingsHandler(nil, nil, nil, nil, logger).WithAudit(rec).WithSessions(rev)
		rr := call(h, true)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Signed out 3 other sessions.") {
			t.Fatalf("status=%d body=%q", rr.Code, rr.Body.String())
		}
		if rev.userID != user.ID || rev.keep != sess.ID {
			t.Errorf("revoker got user=%q keep=%q", rev.userID, rev.keep)
		}
		rows := rec.snapshot()
		if len(rows) != 1 || rows[0].action != models.AuditActionUserSessionsRevoke ||
			rows[0].details["sessions_revoked"] != int64(3) || rows[0].details["via"] != models.AuditViaUI {
			t.Errorf("audit rows = %+v", rows)
		}
	})

	t.Run("no session keeps none", func(t *testing.T) {
		rev := &fakeRevoker{}
		h := NewSettingsHandler(nil, nil, nil, nil, logger).WithSessions(rev)
		if rr := call(h, false); rr.Code != http.StatusOK {
			t.Fatalf("status=%d", rr.Code)
		}
		if rev.keep != "" {
			t.Errorf("keep = %q, want empty", rev.keep)
		}
	})

	t.Run("store error", func(t *testing.T) {
		rev := &fakeRevoker{err: errors.New("db down")}
		rec := &fakeUIAudit{}
		h := NewSettingsHandler(nil, nil, nil, nil, logger).WithAudit(rec).WithSessions(rev)
		if rr := call(h, true); rr.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d", rr.Code)
		}
		if n := len(rec.snapshot()); n != 0 {
			t.Errorf("error wrote %d audit rows", n)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		rev := &fakeRevoker{}
		h := NewSettingsHandler(nil, nil, nil, nil, logger).WithSessions(rev)
		req := httptest.NewRequest(http.MethodPost, "/settings/sessions/revoke-others", nil)
		rr := httptest.NewRecorder()
		h.RevokeOtherSessions(rr, req)
		if rr.Code != http.StatusUnauthorized || rev.calls != 0 {
			t.Fatalf("status=%d calls=%d", rr.Code, rev.calls)
		}
	})
}
