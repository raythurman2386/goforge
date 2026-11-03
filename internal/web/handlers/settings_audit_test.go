package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// settingsAuditDB opens DATABASE_URL (skipping when no database is
// reachable), like the repository tests.
func settingsAuditDB(t *testing.T) *database.DB {
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
	return db
}

func settingsAuditRequest(method, target string, form url.Values, user *models.User, params map[string]string) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c := context.WithValue(req.Context(), auth.UserContextKey, user)
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for k, v := range params {
			rctx.URLParams.Add(k, v)
		}
		c = context.WithValue(c, chi.RouteCtxKey, rctx)
	}
	return req.WithContext(c)
}

// assertNoSecretInDetails fails when any detail key or value carries
// needle (a token, key, URL, or full address).
func assertNoSecretInDetails(t *testing.T, d map[string]interface{}, needles ...string) {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range needles {
		if strings.Contains(string(raw), n) {
			t.Errorf("audit details %s carry %q", raw, n)
		}
	}
}

// Creating and deleting a Git source writes git_source.create and
// git_source.delete for that source, with the name and type but never the
// access token or API URL. A refused delete (another account's source)
// writes nothing.
func TestSettingsHandler_GitSource_WritesAuditRows(t *testing.T) {
	db := settingsAuditDB(t)
	ctx := context.Background()
	enc, err := secrets.NewEncryptor([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	users := repositories.NewUserRepository(db.DB, nil)
	sources := repositories.NewGitSourceRepository(db.DB, enc)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	owner := &models.User{Email: "gs-audit-" + uuid.NewString() + "@example.com"}
	other := &models.User{Email: "gs-audit-other-" + uuid.NewString() + "@example.com"}
	for _, u := range []*models.User{owner, other} {
		if err = users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}

	rec := &fakeUIAudit{}
	h := NewSettingsHandler(sources, nil, users, nil, logger).WithAudit(rec)

	const token = "ghp_secret_token_value_t1870u"
	const apiURL = "https://git.internal.example.test/api"
	form := url.Values{"name": {"audit-src"}, "type": {"gitea"}, "api_url": {apiURL}, "access_token": {token}}
	rr := httptest.NewRecorder()
	h.CreateGitSource(rr, settingsAuditRequest(http.MethodPost, "/settings/git", form, owner, nil))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", rr.Code, rr.Body.String())
	}
	list, err := sources.ListByUserID(ctx, owner.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("sources = %v, err=%v", list, err)
	}
	src := list[0]

	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows after create = %+v", rows)
	}
	row := rows[0]
	if row.action != models.AuditActionGitSourceCreate || row.userID != owner.ID ||
		row.resourceType != models.ResourceTypeGitSource || row.resourceID != src.ID {
		t.Errorf("create row = %+v", row)
	}
	if row.details["name"] != "audit-src" || row.details["type"] != "gitea" || row.details["via"] != models.AuditViaUI ||
		row.details["has_access_token"] != true || row.details["has_api_url"] != true {
		t.Errorf("create details = %#v", row.details)
	}
	assertNoSecretInDetails(t, row.details, token, apiURL, "git.internal")

	// Another account cannot delete it, and the refusal writes nothing.
	rr = httptest.NewRecorder()
	h.DeleteGitSource(rr, settingsAuditRequest(http.MethodDelete, "/settings/git/"+src.ID, nil, other, map[string]string{"id": src.ID}))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("foreign delete: status=%d", rr.Code)
	}
	if n := len(rec.snapshot()); n != 1 {
		t.Fatalf("refused delete wrote rows: %d", n)
	}

	rr = httptest.NewRecorder()
	h.DeleteGitSource(rr, settingsAuditRequest(http.MethodDelete, "/settings/git/"+src.ID, nil, owner, map[string]string{"id": src.ID}))
	if rr.Code != http.StatusOK {
		t.Fatalf("delete: status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows = rec.snapshot()
	if len(rows) != 2 {
		t.Fatalf("audit rows after delete = %+v", rows)
	}
	row = rows[1]
	if row.action != models.AuditActionGitSourceDelete || row.userID != owner.ID ||
		row.resourceType != models.ResourceTypeGitSource || row.resourceID != src.ID {
		t.Errorf("delete row = %+v", row)
	}
	if row.details["name"] != "audit-src" || row.details["type"] != "gitea" || row.details["via"] != models.AuditViaUI {
		t.Errorf("delete details = %#v", row.details)
	}
	assertNoSecretInDetails(t, row.details, token, apiURL, "git.internal")
}

// A refused Git source create (bad type) writes no audit row.
func TestSettingsHandler_CreateGitSource_RefusedWritesNothing(t *testing.T) {
	rec := &fakeUIAudit{}
	h := NewSettingsHandler(nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil))).WithAudit(rec)
	user := &models.User{ID: uuid.NewString()}
	rr := httptest.NewRecorder()
	h.CreateGitSource(rr, settingsAuditRequest(http.MethodPost, "/settings/git", url.Values{"name": {"x"}, "type": {"svn"}}, user, nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("refused create wrote %d rows", n)
	}
}

// Saving the Profile form writes user.profile.update with the old and new
// email domains only, never the full address. Saving the same address
// writes an empty changed list. An invalid address writes nothing.
func TestSettingsHandler_UpdateProfile_WritesAuditRow(t *testing.T) {
	db := settingsAuditDB(t)
	ctx := context.Background()
	users := repositories.NewUserRepository(db.DB, nil)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	local := "profile-audit-" + uuid.NewString()
	user := &models.User{Email: local + "@old.example.com"}
	if err := users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	rec := &fakeUIAudit{}
	h := NewSettingsHandler(nil, nil, users, nil, logger).WithAudit(rec)

	put := func(email string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		h.UpdateProfile(rr, settingsAuditRequest(http.MethodPut, "/settings/profile", url.Values{"email": {email}}, user, nil))
		return rr
	}

	if rr := put("not-an-address"); rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid: status=%d", rr.Code)
	}
	if n := len(rec.snapshot()); n != 0 {
		t.Fatalf("invalid save wrote %d rows", n)
	}

	newEmail := local + "@New.Example.org"
	if rr := put(newEmail); rr.Code != http.StatusOK {
		t.Fatalf("save: status=%d body=%s", rr.Code, rr.Body.String())
	}
	rows := rec.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %+v", rows)
	}
	row := rows[0]
	if row.action != models.AuditActionUserProfileUpdate || row.userID != user.ID ||
		row.resourceType != models.ResourceTypeUser || row.resourceID != user.ID {
		t.Errorf("row = %+v", row)
	}
	if row.details["old_email_domain"] != "old.example.com" || row.details["new_email_domain"] != "new.example.org" ||
		row.details["via"] != models.AuditViaUI {
		t.Errorf("details = %#v", row.details)
	}
	if ch, ok := row.details["changed"].([]string); !ok || len(ch) != 1 || ch[0] != "email" {
		t.Errorf("changed = %#v", row.details["changed"])
	}
	assertNoSecretInDetails(t, row.details, local)

	// Same address again (case differs only): changed is empty.
	if rr := put(strings.ToUpper(local) + "@new.example.org"); rr.Code != http.StatusOK {
		t.Fatalf("resave: status=%d", rr.Code)
	}
	rows = rec.snapshot()
	if len(rows) != 2 {
		t.Fatalf("audit rows = %+v", rows)
	}
	if ch, ok := rows[1].details["changed"].([]string); !ok || len(ch) != 0 {
		t.Errorf("resave changed = %#v", rows[1].details["changed"])
	}
}

func TestEmailDomain(t *testing.T) {
	for in, want := range map[string]string{
		"a@Example.COM":   "example.com",
		"weird@x@host.io": "host.io",
		"nodomain":        "",
		"":                "",
	} {
		if got := emailDomain(in); got != want {
			t.Errorf("emailDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
