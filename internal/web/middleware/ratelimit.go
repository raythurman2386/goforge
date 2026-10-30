// Package middleware provides HTTP middleware for GoForge.
package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/raythurman2386/goforge/internal/web/httputil"
)

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter holds the rate limiter state and provides an HTTP middleware.
type RateLimiter struct {
	clients        sync.Map
	rps            float64
	burst          int
	trustedProxies []*net.IPNet
	done           chan struct{}
}

// NewRateLimiter creates a new IP-based rate limiter.
// trustedProxies controls which peers' CF-Connecting-IP / X-Forwarded-For
// headers are trusted for extracting the real client IP (httputil.ClientIP). Pass nil to only use RemoteAddr.
// Call Stop() during graceful shutdown to release the cleanup goroutine.
func NewRateLimiter(rps float64, burst int, trustedProxies []*net.IPNet) *RateLimiter {
	rl := &RateLimiter{
		rps:            rps,
		burst:          burst,
		trustedProxies: trustedProxies,
		done:           make(chan struct{}),
	}

	// Cleanup background routine
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.clients.Range(func(key, value interface{}) bool {
					if c, ok := value.(*client); ok {
						if time.Since(c.lastSeen) > 3*time.Minute {
							rl.clients.Delete(key)
						}
					}
					return true
				})
			case <-rl.done:
				return
			}
		}
	}()

	return rl
}

// Stop stops the cleanup goroutine. Safe to call multiple times.
func (rl *RateLimiter) Stop() {
	select {
	case <-rl.done:
		// already closed
	default:
		close(rl.done)
	}
}

// Middleware returns the chi-compatible middleware handler.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := httputil.ClientIP(r, rl.trustedProxies)

		var c *client
		if v, ok := rl.clients.Load(ip); ok {
			if typed, ok := v.(*client); ok {
				c = typed
			}
		}

		if c == nil {
			c = &client{limiter: rate.NewLimiter(rate.Limit(rl.rps), rl.burst)}
			rl.clients.Store(ip, c)
		}

		c.lastSeen = time.Now()

		if !c.limiter.Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
