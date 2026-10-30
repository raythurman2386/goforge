// Package middleware provides HTTP middleware for GoForge.
package middleware

import (
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
	clients sync.Map
	rps     float64
	burst   int
	trust   httputil.ProxyTrust
	done    chan struct{}
}

// NewRateLimiter creates a new IP-based rate limiter.
// trust controls which proxy headers are read for the client IP
// (httputil.ClientIP). The zero value only uses RemoteAddr.
// Call Stop() during graceful shutdown to release the cleanup goroutine.
func NewRateLimiter(rps float64, burst int, trust httputil.ProxyTrust) *RateLimiter {
	rl := &RateLimiter{
		rps:   rps,
		burst: burst,
		trust: trust,
		done:  make(chan struct{}),
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
		ip := httputil.ClientIP(r, rl.trust)

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
