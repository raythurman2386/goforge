package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

// fakeSampler returns successive samples whose CPU and system counters grow
// by fixed steps, so the delta between any two is 25% on one CPU.
type fakeSampler struct {
	mu    sync.Mutex
	calls int
	err   error
	stop  bool // return the empty sample Docker sends for a stopped container
	times []time.Time
}

func (f *fakeSampler) SampleStats(_ context.Context, _ string) (*container.StatsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.times = append(f.times, time.Now())
	if f.err != nil {
		return nil, f.err
	}
	if f.stop {
		return &container.StatsResponse{}, nil
	}
	n := uint64(f.calls)
	return &container.StatsResponse{
		Read: time.Now(),
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 250 * n},
			SystemUsage: 1000 * n,
			OnlineCPUs:  1,
		},
		MemoryStats: container.MemoryStats{Usage: 64 << 20, Limit: 256 << 20},
		Networks:    map[string]container.NetworkStats{"eth0": {RxBytes: 2048, TxBytes: 1024}},
	}, nil
}

func runStatsStream(t *testing.T, sampler statsSampler, d time.Duration) string {
	t.Helper()
	h := NewSSEHandler(nil, nil, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if sampler != nil {
		h.stats = sampler
	}
	h.ownsContainer = func(context.Context, string, string) (bool, error) { return true, nil }

	req := httptest.NewRequest(http.MethodGet, "/containers/abc123/stats", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc123")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, auth.UserContextKey, &models.User{ID: "user-1"})
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.ContainerStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	return w.Body.String()
}

func TestContainerStats_FirstFrameWithinOneSecond(t *testing.T) {
	t.Parallel()
	f := &fakeSampler{}
	// The stream ends after 1s; the old handler waited for a 2s tick, so it
	// sent nothing in this window.
	body := runStatsStream(t, f, time.Second)

	if got := strings.Count(body, "event: stats\n"); got != 1 {
		t.Fatalf("stats events in 1s = %d, want 1:\n%s", got, body)
	}
	// Two one-shot samples give a real CPU delta: 250/1000 on one CPU.
	if !strings.Contains(body, "25.00%") {
		t.Errorf("first frame CPU is not the two-sample delta (want 25.00%%):\n%s", body)
	}
	if !strings.Contains(body, "Network I/O") || !strings.Contains(body, "2.0 KiB in / 1.0 KiB out") {
		t.Errorf("first frame lacks network I/O:\n%s", body)
	}
	if strings.Contains(body, "event: unavailable") {
		t.Errorf("unexpected unavailable event:\n%s", body)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.times) < 2 {
		t.Fatalf("samples = %d, want 2", len(f.times))
	}
	if gap := f.times[1].Sub(f.times[0]); gap > 900*time.Millisecond {
		t.Errorf("second sample came %v after the first, want under 1s", gap)
	}
}

func TestContainerStats_MissingContainerSendsUnavailable(t *testing.T) {
	t.Parallel()
	body := runStatsStream(t, &fakeSampler{err: errors.New("No such container: abc123")}, 300*time.Millisecond)
	if !strings.Contains(body, "event: unavailable\n") {
		t.Fatalf("no unavailable event for a missing container:\n%s", body)
	}
	if !strings.Contains(body, "the container was not found") || !strings.Contains(body, "data-stats-unavailable") {
		t.Errorf("unavailable frame lacks its visible reason:\n%s", body)
	}
	if strings.Contains(body, "event: stats") {
		t.Errorf("stats event for a missing container:\n%s", body)
	}
	if strings.Contains(body, "No such container") {
		t.Errorf("Docker error text leaked into the page:\n%s", body)
	}
}

func TestContainerStats_StoppedContainerSendsUnavailable(t *testing.T) {
	t.Parallel()
	body := runStatsStream(t, &fakeSampler{stop: true}, 300*time.Millisecond)
	if !strings.Contains(body, "event: unavailable\n") || !strings.Contains(body, "the container is not running") {
		t.Fatalf("no not-running unavailable event:\n%s", body)
	}
}

func TestContainerStats_UnavailableSentOncePerReason(t *testing.T) {
	t.Parallel()
	f := &fakeSampler{stop: true}
	h := NewSSEHandler(nil, nil, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.stats = f
	h.statsInterval = 20 * time.Millisecond
	h.ownsContainer = func(context.Context, string, string) (bool, error) { return true, nil }

	req := httptest.NewRequest(http.MethodGet, "/containers/abc123/stats", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc123")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, auth.UserContextKey, &models.User{ID: "user-1"})
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	h.ContainerStats(w, req.WithContext(ctx))

	if got := strings.Count(w.Body.String(), "event: unavailable\n"); got != 1 {
		t.Fatalf("unavailable events = %d, want 1 (sampled %d times)", got, f.calls)
	}
	if f.calls < 3 {
		t.Fatalf("stream stopped sampling after unavailable: %d calls", f.calls)
	}
}

func TestContainerStats_NoCollectorSendsUnavailable(t *testing.T) {
	t.Parallel()
	body := runStatsStream(t, nil, 200*time.Millisecond)
	if !strings.Contains(body, "event: unavailable\n") {
		t.Fatalf("no unavailable event without a collector:\n%s", body)
	}
}

func TestContainerStats_NotOwnedIsForbidden(t *testing.T) {
	t.Parallel()
	h := NewSSEHandler(nil, nil, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.stats = &fakeSampler{}
	h.ownsContainer = func(context.Context, string, string) (bool, error) { return false, nil }
	req := httptest.NewRequest(http.MethodGet, "/containers/abc123/stats", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "abc123")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, auth.UserContextKey, &models.User{ID: "user-2"})
	w := httptest.NewRecorder()
	h.ContainerStats(w, req.WithContext(ctx))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}
