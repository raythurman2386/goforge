package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
)

const otherInstanceID = "660e8400-e29b-41d4-a716-446655440000"

// bucketStore is an in-memory backup.Store.
type bucketStore map[string][]byte

func (b bucketStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	body, err := io.ReadAll(r)
	b[key] = body
	return err
}
func (b bucketStore) List(_ context.Context, prefix string) ([]backup.Object, error) {
	var out []backup.Object
	for k, v := range b {
		if strings.HasPrefix(k, prefix) {
			out = append(out, backup.Object{Key: k, Size: int64(len(v))})
		}
	}
	return out, nil
}
func (b bucketStore) Delete(context.Context, []string) error { return nil }
func (b bucketStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	body, ok := b[key]
	if !ok {
		return nil, backup.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

// putManifest writes a run whose manifest lists vaultwarden artifacts of
// bytes each for the given instances.
func putManifest(t *testing.T, s bucketStore, runKey string, started time.Time, bytes int64, instances ...string) {
	t.Helper()
	m := backup.Manifest{Version: 1, RunKey: runKey, Trigger: "scheduled", StartedAt: started,
		Artifacts: []backup.ManifestArtifact{{Key: backup.AppDumpKey, Kind: backup.KindPgDump, Bytes: 999_999}}}
	for _, id := range instances {
		m.Artifacts = append(m.Artifacts,
			backup.ManifestArtifact{Key: "services/" + id + "/vaultwarden.sqlite3", Kind: backup.KindSQLite, InstanceID: id, Template: "vaultwarden", Bytes: bytes},
			backup.ManifestArtifact{Key: "services/" + id + "/data.tar.gz", Kind: backup.KindTarGz, InstanceID: id, Template: "vaultwarden", Bytes: bytes})
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s["goforge/"+runKey+"/"+backup.AppDumpKey] = []byte("x")
	s["goforge/"+runKey+"/"+backup.ManifestName] = body
}

func serviceRestorePointsLister(t *testing.T) *backup.RestorePointLister {
	t.Helper()
	s := bucketStore{}
	putManifest(t, s, "2026-10-28/20261028T070000Z", time.Date(2026, 10, 28, 7, 0, 0, 0, time.UTC), 1024, editTestInstanceID, otherInstanceID)
	putManifest(t, s, "2026-10-29/20261029T070000Z", time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC), 4096, otherInstanceID)
	putManifest(t, s, "2026-10-30/20261030T070000Z", time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC), 2048, editTestInstanceID)
	cfg := config.BackupConfig{Bucket: "bk", Prefix: "goforge/", Schedule: "02:00 America/Chicago"}
	return &backup.RestorePointLister{
		Current:  func() (config.BackupConfig, string) { return cfg, backup.SourceSettings },
		NewStore: func(config.BackupConfig) (backup.Store, error) { return s, nil },
	}
}

func restorePointsRequest(u *models.User) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/services/"+editTestInstanceID+"/restore-points", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", editTestInstanceID)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if u != nil {
		ctx = context.WithValue(ctx, auth.UserContextKey, u)
	}
	return req.WithContext(ctx)
}

func TestServiceRestorePoints_FiltersByInstance(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	h.WithRestorePoints(serviceRestorePointsLister(t))
	rr := httptest.NewRecorder()
	h.RestorePoints(rr, restorePointsRequest(&models.User{ID: "owner", Role: models.RoleAdmin}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	body := rr.Body.String()
	if n := strings.Count(body, `data-testid="service-restore-point"`); n != 2 {
		t.Errorf("rows = %d, want 2 (only runs holding this instance):\n%s", n, body)
	}
	for _, want := range []string{
		"2026-10-30/20261030T070000Z", "2026-10-28/20261028T070000Z", // run keys, newest first
		"Oct 30, 2026 02:00 CDT", // time in the schedule's zone
		"4.0 KiB", "2.0 KiB",     // this instance's artifacts only (2 x 2048, 2 x 1024)
		"docker compose exec goforge ./goforge backup restore-service 2026-10-30/20261030T070000Z " + editTestInstanceID,
		"restore-service 2026-10-30/20261030T070000Z " + editTestInstanceID + " --yes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "2026-10-29/20261029T070000Z") || strings.Contains(body, otherInstanceID) {
		t.Errorf("shows another instance's run:\n%s", body)
	}
	if strings.Index(body, "2026-10-30/20261030T070000Z") > strings.Index(body, "2026-10-28/20261028T070000Z") {
		t.Error("not newest first")
	}
	if strings.Contains(body, "976.6 KiB") || strings.Contains(body, "1.0 MiB") {
		t.Error("size includes the app dump")
	}
}

func TestServiceRestorePoints_AdminOwnerOnly(t *testing.T) {
	h, _ := newEditHandler(t, models.ServiceStatusRunning)
	calls := 0
	h.WithRestorePoints(countingLister{inner: serviceRestorePointsLister(t), calls: &calls})
	for _, tc := range []struct {
		name string
		user *models.User
		want int
	}{
		{"anonymous", nil, http.StatusUnauthorized},
		{"owner, not admin", &models.User{ID: "owner", Role: models.RoleUser}, http.StatusForbidden},
		{"admin, not owner", &models.User{ID: "someone-else", Role: models.RoleAdmin}, http.StatusForbidden},
	} {
		rr := httptest.NewRecorder()
		h.RestorePoints(rr, restorePointsRequest(tc.user))
		if rr.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", tc.name, rr.Code, tc.want)
		}
	}
	if calls != 0 {
		t.Fatalf("listed the bucket %d time(s) for a refused request", calls)
	}

	// Without a lister the route is 404.
	h2, _ := newEditHandler(t, models.ServiceStatusRunning)
	rr := httptest.NewRecorder()
	h2.RestorePoints(rr, restorePointsRequest(&models.User{ID: "owner", Role: models.RoleAdmin}))
	if rr.Code != http.StatusNotFound {
		t.Errorf("no lister: status = %d", rr.Code)
	}
}

type countingLister struct {
	inner *backup.RestorePointLister
	calls *int
}

func (c countingLister) ListInstance(ctx context.Context, id string) *backup.InstanceRestorePointsView {
	*c.calls++
	return c.inner.ListInstance(ctx, id)
}

func TestServiceShow_RestorePointsCard(t *testing.T) {
	show := func(u *models.User, withLister bool) string {
		h, _ := newEditHandler(t, models.ServiceStatusRunning)
		if withLister {
			h.WithRestorePoints(serviceRestorePointsLister(t))
		}
		req := restorePointsRequest(u)
		rr := httptest.NewRecorder()
		h.Show(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("show status = %d", rr.Code)
		}
		return rr.Body.String()
	}
	admin := &models.User{ID: "owner", Role: models.RoleAdmin}
	html := show(admin, true)
	if !strings.Contains(html, `data-testid="service-restore-points"`) || !strings.Contains(html, `hx-get="/services/`+editTestInstanceID+`/restore-points"`) {
		t.Error("admin owner does not get the Restore points card")
	}
	if strings.Contains(html, "20261030T070000Z") {
		t.Error("the page lists the bucket inline; it must load the card after the page")
	}
	if html := show(&models.User{ID: "owner", Role: models.RoleUser}, true); strings.Contains(html, "service-restore-points") {
		t.Error("non-admin owner sees the Restore points card")
	}
	if html := show(admin, false); strings.Contains(html, "service-restore-points") {
		t.Error("card shown without a lister")
	}
}
