package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/sse"
)

func TestSSEHandler_DeploymentLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		deploymentID   string
		wantStatus     int
		wantSSEHeaders bool
	}{
		{
			name:           "valid deployment returns SSE stream",
			deploymentID:   "deploy-123",
			wantStatus:     http.StatusOK,
			wantSSEHeaders: true,
		},
		{
			name:           "empty deployment ID returns 400",
			deploymentID:   "",
			wantStatus:     http.StatusBadRequest,
			wantSSEHeaders: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hub := sse.NewHub()
			go hub.Run()
			defer hub.Stop()

			handler := NewSSEHandler(hub, nil, nil)

			// Build URL with chi context
			path := "/deployments/" + tt.deploymentID + "/logs"
			if tt.deploymentID == "" {
				path = "/deployments//logs"
			}

			req := httptest.NewRequest("GET", path, nil)

			// Add chi URL params
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("deploymentID", tt.deploymentID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			// Use a cancellable context to end the SSE stream
			ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
			defer cancel()
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			handler.DeploymentLogs(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantSSEHeaders {
				contentType := resp.Header.Get("Content-Type")
				if !strings.Contains(contentType, "text/event-stream") {
					t.Errorf("Content-Type = %q, want text/event-stream", contentType)
				}
			}
		})
	}
}

func TestSSEHandler_ContainerLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		containerID    string
		wantStatus     int
		wantSSEHeaders bool
	}{
		{
			name:           "valid container returns SSE stream",
			containerID:    "container-456",
			wantStatus:     http.StatusOK,
			wantSSEHeaders: true,
		},
		{
			name:           "empty container ID returns 400",
			containerID:    "",
			wantStatus:     http.StatusBadRequest,
			wantSSEHeaders: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hub := sse.NewHub()
			go hub.Run()
			defer hub.Stop()

			handler := NewSSEHandler(hub, nil, nil)

			path := "/containers/" + tt.containerID + "/logs"
			if tt.containerID == "" {
				path = "/containers//logs"
			}

			req := httptest.NewRequest("GET", path, nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.containerID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
			defer cancel()
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()

			handler.ContainerLogs(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantSSEHeaders {
				contentType := resp.Header.Get("Content-Type")
				if !strings.Contains(contentType, "text/event-stream") {
					t.Errorf("Content-Type = %q, want text/event-stream", contentType)
				}
			}
		})
	}
}

func TestSSEHandler_BuildOutput(t *testing.T) {
	t.Parallel()

	hub := sse.NewHub()
	go hub.Run()
	defer hub.Stop()

	handler := NewSSEHandler(hub, nil, nil)

	req := httptest.NewRequest("GET", "/builds/build-789/output", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "build-789")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()

	handler.BuildOutput(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", contentType)
	}
}

func TestSSEHandler_RequiresFlush(t *testing.T) {
	t.Parallel()

	hub := sse.NewHub()
	go hub.Run()
	defer hub.Stop()

	handler := NewSSEHandler(hub, nil, nil)

	req := httptest.NewRequest("GET", "/deployments/deploy-123/logs", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("deploymentID", "deploy-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	// Use httptest.ResponseRecorder which implements Flusher
	w := httptest.NewRecorder()

	handler.DeploymentLogs(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Check for cache-control and connection headers
	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cacheControl)
	}

	connection := resp.Header.Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", connection)
	}
}
