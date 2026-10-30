package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
)

const testBackupSecret = "SECRET-never-in-html-0123456789"

type fakeBackupSettings struct {
	mu      sync.Mutex
	saved   []backup.SettingsInput
	tested  []backup.SettingsInput
	saveErr error
	testErr error
	changed []string
}

func (f *fakeBackupSettings) View(context.Context) (*backup.SettingsView, error) {
	return backup.NewSettingsView(config.BackupConfig{
		Endpoint: "https://s3.example.com", Region: "auto", Bucket: "bk", Prefix: "goforge/",
		AccessKeyID: "AKIAEXAMPLEKEYID9876", SecretAccessKey: testBackupSecret,
		Schedule: "02:00 America/Chicago", RetentionDaily: 7,
	}, backup.SourceSettings), nil
}

func (f *fakeBackupSettings) Save(_ context.Context, in backup.SettingsInput, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, in)
	return f.changed, f.saveErr
}

func (f *fakeBackupSettings) Test(_ context.Context, in backup.SettingsInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tested = append(f.tested, in)
	return "Connected.", f.testErr
}

type fakeAudit struct {
	mu      sync.Mutex
	actions []string
	details []map[string]interface{}
}

func (f *fakeAudit) Record(_ context.Context, _, action, _, _ string, details map[string]interface{}, _, _ string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	f.details = append(f.details, details)
}

type staticStatus struct{}

func (staticStatus) Status(context.Context) (*backup.Status, error) {
	return &backup.Status{Enabled: true, Bucket: "bk", Prefix: "goforge/", Schedule: "02:00 America/Chicago", RetentionDaily: 7}, nil
}

func backupHandler(s *fakeBackupSettings, a *fakeAudit) *SettingsHandler {
	return (&SettingsHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).
		WithBackups(staticStatus{}).WithBackupSettings(s, a)
}

func backupForm(extra url.Values) url.Values {
	v := url.Values{
		"enabled": {"true"}, "endpoint": {"https://s3.example.com"}, "region": {"auto"},
		"bucket": {"bk"}, "prefix": {"goforge/"}, "schedule_time": {"03:00"},
		"schedule_zone": {"UTC"}, "retention_daily": {"7"}, "timeout": {"2h"},
	}
	for k, vals := range extra {
		v[k] = vals
	}
	return v
}

func backupReq(method, path string, form url.Values, user *models.User) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user))
	}
	return req
}

var (
	adminUser  = &models.User{ID: "11111111-1111-1111-1111-111111111111", Email: "a@x", Role: models.RoleAdmin}
	plainUser  = &models.User{ID: "22222222-2222-2222-2222-222222222222", Email: "u@x", Role: models.RoleUser}
	backupPath = "/settings/backups"
)

func TestUpdateBackupSettings_NonAdminForbidden(t *testing.T) {
	s, a := &fakeBackupSettings{}, &fakeAudit{}
	h := backupHandler(s, a)
	for _, tc := range []struct {
		name string
		user *models.User
		want int
	}{{"anonymous", nil, http.StatusUnauthorized}, {"non-admin", plainUser, http.StatusForbidden}} {
		for _, call := range []struct {
			path string
			fn   http.HandlerFunc
		}{{backupPath, h.UpdateBackupSettings}, {backupPath + "/test", h.TestBackupConnection}} {
			rr := httptest.NewRecorder()
			call.fn(rr, backupReq(http.MethodPut, call.path, backupForm(nil), tc.user))
			if rr.Code != tc.want {
				t.Errorf("%s %s = %d, want %d", tc.name, call.path, rr.Code, tc.want)
			}
		}
	}
	if len(s.saved) != 0 || len(s.tested) != 0 || len(a.actions) != 0 {
		t.Fatalf("non-admin reached the service: saved=%d tested=%d audit=%d", len(s.saved), len(s.tested), len(a.actions))
	}
}

func TestUpdateBackupSettings_SuccessAuditsNamesOnly(t *testing.T) {
	s := &fakeBackupSettings{changed: []string{"bucket", "secret_access_key"}}
	a := &fakeAudit{}
	rr := httptest.NewRecorder()
	backupHandler(s, a).UpdateBackupSettings(rr, backupReq(http.MethodPut, backupPath,
		backupForm(url.Values{"secret_access_key": {"rotated-secret-value"}, "force_path_style": {"true"}}), adminUser))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	body := rr.Body.String()
	if strings.Contains(body, "rotated-secret-value") || strings.Contains(body, testBackupSecret) {
		t.Fatal("secret in response")
	}
	if !strings.Contains(body, `id="backup-status"`) || !strings.Contains(body, "Backup settings saved") {
		t.Errorf("body = %s", body)
	}
	in := s.saved[0]
	if !in.Enabled || !in.ForcePathStyle || in.SecretAccessKey != "rotated-secret-value" || in.ScheduleTime != "03:00" {
		t.Errorf("input = %+v", in)
	}
	if len(a.actions) != 1 || a.actions[0] != "backup.settings.update" {
		t.Fatalf("audit = %v", a.actions)
	}
	got := a.details[0]["changed"].([]string)
	if strings.Join(got, ",") != "bucket,secret_access_key" || len(a.details[0]) != 1 {
		t.Errorf("audit details = %v", a.details[0])
	}
}

func TestUpdateBackupSettings_ValidationError(t *testing.T) {
	s := &fakeBackupSettings{saveErr: &config.BackupFieldError{Field: "retention_daily", Message: "must be between 1 and 366, got 0"}}
	a := &fakeAudit{}
	rr := httptest.NewRecorder()
	backupHandler(s, a).UpdateBackupSettings(rr, backupReq(http.MethodPut, backupPath, backupForm(nil), adminUser))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Keep daily backups must be between 1 and 366") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	if len(a.actions) != 0 {
		t.Error("audited a failed save")
	}
}

func TestTestBackupConnection(t *testing.T) {
	s := &fakeBackupSettings{}
	rr := httptest.NewRecorder()
	backupHandler(s, nil).TestBackupConnection(rr, backupReq(http.MethodPost, backupPath+"/test", backupForm(nil), adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Connected.") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	s.testErr = &backup.ProbeError{Message: "Access Denied"}
	rr = httptest.NewRecorder()
	backupHandler(s, nil).TestBackupConnection(rr, backupReq(http.MethodPost, backupPath+"/test", backupForm(nil), adminUser))
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "Connection failed: Access Denied") {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
}

// A PUT without a CSRF token never reaches the handler.
func TestUpdateBackupSettings_CSRF(t *testing.T) {
	s, a := &fakeBackupSettings{}, &fakeAudit{}
	h := auth.NewCSRF("csrf-test-secret").Middleware(http.HandlerFunc(backupHandler(s, a).UpdateBackupSettings))
	sess := &models.Session{TokenHash: "0123456789abcdef"}
	withSession := func(r *http.Request) *http.Request {
		ctx := context.WithValue(r.Context(), auth.SessionContextKey, sess)
		return r.WithContext(ctx)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, withSession(backupReq(http.MethodPut, backupPath, backupForm(nil), adminUser)))
	if rr.Code != http.StatusForbidden || len(s.saved) != 0 {
		t.Fatalf("no token: status = %d saved=%d", rr.Code, len(s.saved))
	}

	// The token a signed-in page would carry.
	var token string
	auth.NewCSRF("csrf-test-secret").Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		token = auth.GetCSRFToken(r)
	})).ServeHTTP(httptest.NewRecorder(), withSession(httptest.NewRequest(http.MethodGet, "/settings/profile", nil)))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, withSession(backupReq(http.MethodPut, backupPath, backupForm(url.Values{"csrf_token": {token}}), adminUser)))
	if rr.Code != http.StatusOK || len(s.saved) != 1 {
		t.Fatalf("with token: status = %d saved=%d body=%s", rr.Code, len(s.saved), rr.Body)
	}
}
