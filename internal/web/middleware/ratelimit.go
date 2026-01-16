// Package middleware provides HTTP middleware for GoForge.
package middleware

import (
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimit implements a simple IP-based rate limiter using a token bucket.
func RateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	clients := sync.Map{}

	// Cleanup background routine
	go func() {
		for {
			time.Sleep(time.Minute)
			clients.Range(func(key, value interface{}) bool {
				if c, ok := value.(*client); ok {
					if time.Since(c.lastSeen) > 3*time.Minute {
						clients.Delete(key)
					}
				}
				return true
			})
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			// In production behind a proxy (like Traefik), you might want X-Forwarded-For.
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				ip = forwarded
			}

			var c *client
			if v, ok := clients.Load(ip); ok {
				if typed, ok := v.(*client); ok {
					c = typed
				}
			}

			if c == nil {
				c = &client{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
				clients.Store(ip, c)
			}

			c.lastSeen = time.Now()

			if !c.limiter.Allow() {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
