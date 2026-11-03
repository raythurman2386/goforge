package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

type fakePruner struct {
	summary docker.PruneSummary
	err     error
	calls   int
}

func (f *fakePruner) PruneSystem(context.Context) (docker.PruneSummary, error) {
	f.calls++
	return f.summary, f.err
}

type fakeStatsCollector struct{ stats *models.SystemStats }

func (f fakeStatsCollector) CollectSystemStats(context.Context, string) (*models.SystemStats, error) {
	return f.stats, nil
}

func newPruneHandler(p *fakePruner) *DashboardHandler {
	// After the prune nothing is reclaimable, so the re-rendered card shows
	// the disabled button.
	stats := &models.SystemStats{DiskUsage: &models.DiskUsageInfo{ImagesSize: 1 << 20}}
	return &DashboardHandler{
		collector:       fakeStatsCollector{stats: stats},
		instanceService: p,
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func pruneRequest(user *models.User, htmx bool) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/dashboard/prune", nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserContextKey, user))
	}
	return req
}

var pruneAdmin = &models.User{ID: "a", Role: models.RoleAdmin}

// t1905u: an htmx prune answers 200 with the re-rendered #dashboard-stats
// and the summary toast out of band, so the card refreshes in place.
func TestDashboardPrune_HTMXSuccess(t *testing.T) {
	p := &fakePruner{summary: docker.PruneSummary{SpaceReclaimed: 3672645263, Containers: 2, Images: 5, BuildCacheRecords: 41}}
	rr := httptest.NewRecorder()
	newPruneHandler(p).Prune(rr, pruneRequest(pruneAdmin, true))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`id="dashboard-stats"`,
		`id="toast" hx-swap-oob="innerHTML"`,
		"Pruned 3.42 GB: 2 containers, 5 images, 41 build cache records",
		"Nothing to prune",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if p.calls != 1 {
		t.Fatalf("PruneSystem calls = %d, want 1", p.calls)
	}
}

// t1905u: nothing reclaimed reads "Nothing to free", not "Pruned 0 B".
func TestDashboardPrune_HTMXNothingToFree(t *testing.T) {
	rr := httptest.NewRecorder()
	newPruneHandler(&fakePruner{}).Prune(rr, pruneRequest(pruneAdmin, true))
	body := rr.Body.String()
	if !strings.Contains(body, "Nothing to free") || strings.Contains(body, "Pruned 0 B") {
		t.Fatalf("zero prune toast wrong: %s", body)
	}
}

// t1905u: a failed prune is a RenderError fragment (500), which app.js shows
// as an error toast; the Docker error text is not sent to the browser.
func TestDashboardPrune_HTMXError(t *testing.T) {
	p := &fakePruner{err: errors.New("failed to prune images: daemon secret detail")}
	rr := httptest.NewRecorder()
	newPruneHandler(p).Prune(rr, pruneRequest(pruneAdmin, true))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, pruneFailedMessage) {
		t.Errorf("body missing error message: %s", body)
	}
	if !strings.Contains(body, `role="alert"`) {
		t.Errorf("body is not the RenderError alert fragment: %s", body)
	}
	if strings.Contains(body, "daemon secret detail") || strings.Contains(body, "<!DOCTYPE") || strings.Contains(body, `id="dashboard-stats"`) {
		t.Errorf("error body leaks Docker text or renders a page: %s", body)
	}
}

// A plain form POST (no JS) still redirects to /dashboard.
func TestDashboardPrune_NonHTMXRedirects(t *testing.T) {
	p := &fakePruner{summary: docker.PruneSummary{SpaceReclaimed: 10}}
	rr := httptest.NewRecorder()
	newPruneHandler(p).Prune(rr, pruneRequest(pruneAdmin, false))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", loc)
	}
}

// A signed-in non-admin gets 403 and nothing is pruned.
func TestDashboardPrune_NonAdminForbidden(t *testing.T) {
	p := &fakePruner{}
	rr := httptest.NewRecorder()
	newPruneHandler(p).Prune(rr, pruneRequest(&models.User{ID: "u", Role: models.RoleUser}, true))

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	if p.calls != 0 {
		t.Fatal("non-admin request pruned")
	}
}

// summaryDocker is fakeDocker with a CleanupSystem that reports a summary.
type summaryDocker struct {
	fakeDocker
	summary docker.PruneSummary
	err     error
}

func (d summaryDocker) CleanupSystem(context.Context) (docker.PruneSummary, error) {
	return d.summary, d.err
}

// t1905u: POST /settings/prune keeps its toast and uses the same summary text.
func TestSettingsPrune_SummaryToast(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sum := docker.PruneSummary{SpaceReclaimed: 5 << 20, Images: 2, BuildCacheRecords: 3}
	is := services.NewInstanceService(nil, nil, summaryDocker{summary: sum}, nil, "goforge_net", false, logger)
	h := &SettingsHandler{instanceService: is, logger: logger}

	rr := httptest.NewRecorder()
	h.PruneSystem(rr, pruneRequest(pruneAdmin, true))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{`id="toast" hx-swap-oob="innerHTML"`, "Pruned 5.00 MB: 0 containers, 2 images, 3 build cache records"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %s", want, body)
		}
	}

	is = services.NewInstanceService(nil, nil, summaryDocker{err: errors.New("boom")}, nil, "goforge_net", false, logger)
	h = &SettingsHandler{instanceService: is, logger: logger}
	rr = httptest.NewRecorder()
	h.PruneSystem(rr, pruneRequest(pruneAdmin, true))
	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), pruneFailedMessage) {
		t.Fatalf("error status = %d body = %s", rr.Code, rr.Body.String())
	}
}
