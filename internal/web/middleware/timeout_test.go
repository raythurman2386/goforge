package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTimeout_CompletesBeforeDeadline(t *testing.T) {
	handler := Timeout(1 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); body != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestTimeout_ExceedsDeadline(t *testing.T) {
	handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a slow handler that exceeds the timeout.
		select {
		case <-time.After(5 * time.Second):
			_, _ = w.Write([]byte("should not reach"))
		case <-r.Context().Done():
			// Handler noticed cancellation — expected behavior.
			return
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	body := strings.TrimSpace(rr.Body.String())
	if body != "Service Unavailable" {
		t.Errorf("body = %q, want %q", body, "Service Unavailable")
	}
}

func TestTimeout_CancelsContext(t *testing.T) {
	ctxCanceled := make(chan struct{})

	handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(ctxCanceled)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	select {
	case <-ctxCanceled:
		// Context was canceled as expected.
	case <-time.After(2 * time.Second):
		t.Fatal("handler context was not canceled within expected time")
	}
}

func TestTimeout_SSERouteExcluded(t *testing.T) {
	// Demonstrates that a streaming handler without the Timeout middleware
	// is not subject to write deadlines. We simulate an SSE endpoint that
	// writes over a longer period than the timeout value used elsewhere.
	sseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Write two events with a gap that would exceed a 50ms timeout.
		_, _ = io.WriteString(w, "event: ping\ndata: 1\n\n")
		flusher.Flush()
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, "event: ping\ndata: 2\n\n")
		flusher.Flush()
	})

	// NOT wrapped in Timeout — simulates the SSE route group.
	srv := httptest.NewServer(sseHandler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "data: 2") {
		t.Errorf("SSE stream truncated, expected both events but got:\n%s", body)
	}
}
