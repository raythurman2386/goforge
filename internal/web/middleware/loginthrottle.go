package middleware

import (
	"strings"
	"sync"
	"time"
)

// Default login throttle settings used by cmd/goforge.
const (
	DefaultLoginMaxFailuresPerIP      = 10
	DefaultLoginMaxFailuresPerAccount = 10
	DefaultLoginFailureWindow         = 15 * time.Minute
	DefaultLoginLockout               = 15 * time.Minute
)

type loginAttempts struct {
	failures    int
	windowStart time.Time
	lockedUntil time.Time
}

// LoginThrottle locks out password logins after repeated failures, per client
// IP and per account (lowercased email). While either key is locked, Check
// refuses the attempt before the password is verified (fail closed), so a
// correct guess during a lockout does not succeed.
//
// Failures are counted in a fixed window; reaching the limit locks the key
// for the lockout duration. A successful login clears the account key only:
// clearing the IP key would let an attacker with one valid account reset the
// per-IP counter between guesses.
//
// State is in memory and per process, like RateLimiter. Call Stop during
// shutdown to release the cleanup goroutine.
type LoginThrottle struct {
	mu            sync.Mutex
	entries       map[string]*loginAttempts
	maxPerIP      int
	maxPerAccount int
	window        time.Duration
	lockout       time.Duration
	now           func() time.Time
	done          chan struct{}
	stopOnce      sync.Once
}

// NewLoginThrottle creates a LoginThrottle. A limit of 0 or less disables
// that key type.
func NewLoginThrottle(maxPerIP, maxPerAccount int, window, lockout time.Duration) *LoginThrottle {
	lt := &LoginThrottle{
		entries:       make(map[string]*loginAttempts),
		maxPerIP:      maxPerIP,
		maxPerAccount: maxPerAccount,
		window:        window,
		lockout:       lockout,
		now:           time.Now,
		done:          make(chan struct{}),
	}
	go lt.cleanupLoop()
	return lt
}

// NewDefaultLoginThrottle uses the Default* settings.
func NewDefaultLoginThrottle() *LoginThrottle {
	return NewLoginThrottle(DefaultLoginMaxFailuresPerIP, DefaultLoginMaxFailuresPerAccount,
		DefaultLoginFailureWindow, DefaultLoginLockout)
}

// Stop stops the cleanup goroutine. Safe to call multiple times.
func (lt *LoginThrottle) Stop() {
	lt.stopOnce.Do(func() { close(lt.done) })
}

// Check reports whether a login attempt from ip for account may proceed. When
// it may not, retryAfter is the time left on the longest active lockout.
func (lt *LoginThrottle) Check(ip, account string) (retryAfter time.Duration, allowed bool) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	now := lt.now()
	for _, k := range lt.keys(ip, account) {
		if e, ok := lt.entries[k]; ok && now.Before(e.lockedUntil) {
			if d := e.lockedUntil.Sub(now); d > retryAfter {
				retryAfter = d
			}
		}
	}
	return retryAfter, retryAfter == 0
}

// Failure records a failed login for ip and account.
func (lt *LoginThrottle) Failure(ip, account string) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	now := lt.now()
	if lt.maxPerIP > 0 && ip != "" {
		lt.recordLocked(ipKey(ip), lt.maxPerIP, now)
	}
	if acct := normalizeAccount(account); lt.maxPerAccount > 0 && acct != "" {
		lt.recordLocked(accountKey(acct), lt.maxPerAccount, now)
	}
}

// Success clears the failure count for account.
func (lt *LoginThrottle) Success(account string) {
	acct := normalizeAccount(account)
	if acct == "" {
		return
	}
	lt.mu.Lock()
	defer lt.mu.Unlock()
	delete(lt.entries, accountKey(acct))
}

func (lt *LoginThrottle) recordLocked(key string, limit int, now time.Time) {
	e, ok := lt.entries[key]
	if !ok || now.Sub(e.windowStart) >= lt.window {
		if ok && now.Before(e.lockedUntil) {
			return // still locked; do not extend or reset
		}
		e = &loginAttempts{windowStart: now}
		lt.entries[key] = e
	}
	e.failures++
	if e.failures >= limit {
		e.lockedUntil = now.Add(lt.lockout)
		e.failures = 0
		e.windowStart = now
	}
}

func (lt *LoginThrottle) keys(ip, account string) []string {
	keys := make([]string, 0, 2)
	if lt.maxPerIP > 0 && ip != "" {
		keys = append(keys, ipKey(ip))
	}
	if acct := normalizeAccount(account); lt.maxPerAccount > 0 && acct != "" {
		keys = append(keys, accountKey(acct))
	}
	return keys
}

func (lt *LoginThrottle) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			lt.mu.Lock()
			now := lt.now()
			for k, e := range lt.entries {
				if now.After(e.lockedUntil) && now.Sub(e.windowStart) >= lt.window {
					delete(lt.entries, k)
				}
			}
			lt.mu.Unlock()
		case <-lt.done:
			return
		}
	}
}

func ipKey(ip string) string        { return "ip:" + ip }
func accountKey(acct string) string { return "acct:" + acct }

func normalizeAccount(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
