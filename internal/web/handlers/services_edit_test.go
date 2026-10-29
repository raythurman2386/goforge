package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

const editTestInstanceID = "550e8400-e29b-41d4-a716-446655440000"

type fakeInstanceRepo struct {
	mu   sync.Mutex
	inst models.ServiceInstance
	saw  []models.ServiceInstance
}

func (f *fakeInstanceRepo) Create(context.Context, *models.ServiceInstance) error { return nil }
func (f *fakeInstanceRepo) GetByID(context.Context, string) (*models.ServiceInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := f.inst
	return &cp, nil
}
func (f *fakeInstanceRepo) ListByUserID(context.Context, string) ([]*models.ServiceInstance, error) {
	return nil, nil
}
func (f *fakeInstanceRepo) Update(_ context.Context, s *models.ServiceInstance) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inst = *s
	f.saw = append(f.saw, *s)
	return nil
}
func (f *fakeInstanceRepo) Delete(context.Context, string) error { return nil }

type fakeContainerRepo struct{}

func (fakeContainerRepo) Create(context.Context, *models.Container) error { return nil }
func (fakeContainerRepo) GetByServiceInstanceID(context.Context, string) (*models.Container, error) {
	return &models.Container{ID: "row-1", DockerID: "docker-1"}, nil
}
func (fakeContainerRepo) Delete(context.Context, string) error { return nil }

type fakeDocker struct{}

func (fakeDocker) CreateContainer(context.Context, docker.ContainerOptions) (string, error) {
	return "docker-2", nil
}
func (fakeDocker) StartContainer(context.Context, string) error      { return nil }
func (fakeDocker) StopContainer(context.Context, string) error       { return nil }
func (fakeDocker) RemoveContainer(context.Context, string) error     { return nil }
func (fakeDocker) RestartContainer(context.Context, string) error    { return nil }
func (fakeDocker) CleanupSystem(context.Context) error               { return nil }
func (fakeDocker) EnsureImage(context.Context, string) error         { return nil }
func (fakeDocker) ImageExists(context.Context, string) (bool, error) { return true, nil }

func newEditHandler(t *testing.T, status models.ServiceInstanceStatus) (*ServiceHandler, *fakeInstanceRepo) {
	t.Helper()
	dir := t.TempDir()
	yaml := `name: vaultwarden
display_name: Vaultwarden
web: true
default_version: "latest"
config:
  - name: signups_allowed
    label: Allow Signups
    type: text
    default: "true"
  - name: admin_token
    label: Admin Token
    type: password
    generate: true
docker:
  image: "vaultwarden/server:{{ .Version }}"
  ports:
    - container: 80
  environment:
    SIGNUPS_ALLOWED: "{{ .Config.signups_allowed }}"
    ADMIN_TOKEN: "{{ .Config.admin_token }}"
  volumes:
    - name: data
      path: /data
`
	if err := os.WriteFile(filepath.Join(dir, "vaultwarden.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := services.NewCatalog(dir)
	if err := catalog.LoadTemplates(); err != nil {
		t.Fatal(err)
	}

	cfg, _ := json.Marshal(map[string]interface{}{
		"name": "vw", "domain": "vw.example.com", "signups_allowed": "true", "admin_token": "keep-me",
	})
	repo := &fakeInstanceRepo{inst: models.ServiceInstance{
		ID:           editTestInstanceID,
		UserID:       "owner",
		TemplateName: "vaultwarden",
		Name:         "vw",
		Version:      sql.NullString{String: "latest", Valid: true},
		Config:       cfg,
		Domain:       sql.NullString{String: "vw.example.com", Valid: true},
		Status:       status,
	}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	is := services.NewInstanceService(repo, fakeContainerRepo{}, fakeDocker{}, catalog, "goforge_net", false, logger)
	return NewServiceHandler(catalog, is, logger), repo
}

func editRequest(method, userID string, form url.Values) *http.Request {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, "/services/"+editTestInstanceID+"/edit", body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", editTestInstanceID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, auth.UserContextKey, &models.User{ID: userID})
	return req.WithContext(ctx)
}

func TestServiceHandler_EditConfig_PrefillsWithoutSecrets(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	rr := httptest.NewRecorder()
	h.EditConfig(rr, editRequest(http.MethodGet, "owner", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{`value="vw.example.com"`, `value="true"`, "goforge-vol-" + editTestInstanceID + "-data"} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page missing %q", want)
		}
	}
	if strings.Contains(body, "keep-me") {
		t.Error("edit page leaked the stored password into the form")
	}
}

func TestServiceHandler_UpdateConfig(t *testing.T) {
	tests := []struct {
		name       string
		user       string
		status     models.ServiceInstanceStatus
		wantCode   int
		wantUpdate bool
	}{
		{"owner saves", "owner", models.ServiceStatusRunning, http.StatusSeeOther, true},
		{"other user forbidden", "intruder", models.ServiceStatusRunning, http.StatusForbidden, false},
		{"busy instance conflicts", "owner", models.ServiceStatusPending, http.StatusConflict, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newEditHandler(t, tt.status)
			form := url.Values{
				"csrf_token":      {"tok"},
				"name":            {"vw"},
				"domain":          {"vw.example.com"},
				"signups_allowed": {"false"},
				"admin_token":     {""},
			}
			rr := httptest.NewRecorder()
			h.UpdateConfig(rr, editRequest(http.MethodPost, tt.user, form))

			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantCode)
			}
			repo.mu.Lock()
			defer repo.mu.Unlock()
			if !tt.wantUpdate {
				if len(repo.saw) != 0 {
					t.Errorf("instance updated %d times, want 0", len(repo.saw))
				}
				return
			}
			if loc := rr.Header().Get("Location"); loc != "/services/"+editTestInstanceID {
				t.Errorf("Location = %q", loc)
			}
			if len(repo.saw) == 0 {
				t.Fatal("instance not updated")
			}
			var cfg map[string]interface{}
			if err := json.Unmarshal(repo.saw[0].Config, &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg["signups_allowed"] != "false" || cfg["admin_token"] != "keep-me" {
				t.Errorf("saved config = %v", cfg)
			}
			if _, ok := cfg["csrf_token"]; ok {
				t.Error("csrf_token saved into config")
			}
		})
	}
}
