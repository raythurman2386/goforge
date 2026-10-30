package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raythurman2386/goforge/internal/web/httputil"
)

// A client behind the trusted proxy cannot get a fresh bucket by rotating the
// leftmost X-Forwarded-For entry (XFF-SPOOF).
func TestRateLimiter_SpoofedXFFSharesBucket(t *testing.T) {
	rl := NewRateLimiter(0.0001, 2, httputil.DefaultTrustedProxies())
	t.Cleanup(rl.Stop)
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	codes := make([]int, 0, 4)
	for _, spoof := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "172.18.0.5:40000" // Traefik on the Docker network
		req.Header.Set("X-Forwarded-For", spoof+", 198.51.100.7")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	want := []int{200, 200, 429, 429}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("status codes = %v, want %v", codes, want)
		}
	}
}

// CF-Connecting-IP keys the bucket when present, so two Cloudflare clients
// behind the same tunnel do not share one.
func TestRateLimiter_CFConnectingIPKeys(t *testing.T) {
	rl := NewRateLimiter(0.0001, 1, httputil.DefaultTrustedProxies())
	t.Cleanup(rl.Stop)
	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, ip := range []string{"198.51.100.1", "198.51.100.2"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "172.18.0.5:40000"
		req.Header.Set("CF-Connecting-IP", ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("first request from %s = %d, want 200", ip, rec.Code)
		}
	}
}
