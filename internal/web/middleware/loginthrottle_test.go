package middleware

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestThrottle(t *testing.T, perIP, perAcct int) (*LoginThrottle, *fakeClock) {
	t.Helper()
	lt := NewLoginThrottle(perIP, perAcct, 15*time.Minute, 15*time.Minute)
	t.Cleanup(lt.Stop)
	clk := &fakeClock{t: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	lt.now = clk.now
	return lt, clk
}

func TestLoginThrottle_LocksIPAfterLimit(t *testing.T) {
	lt, clk := newTestThrottle(t, 3, 0)
	for i := 0; i < 3; i++ {
		if _, ok := lt.Check("198.51.100.7", "a@example.test"); !ok {
			t.Fatalf("attempt %d refused before limit", i+1)
		}
		lt.Failure("198.51.100.7", "a@example.test")
	}
	retry, ok := lt.Check("198.51.100.7", "someone-else@example.test")
	if ok {
		t.Fatal("IP not locked after 3 failures")
	}
	if retry != 15*time.Minute {
		t.Errorf("retryAfter = %v, want 15m", retry)
	}
	if _, ok := lt.Check("198.51.100.8", "a@example.test"); !ok {
		t.Error("other IP refused; per-account limit is disabled")
	}

	clk.advance(15*time.Minute + time.Second)
	if _, ok := lt.Check("198.51.100.7", "a@example.test"); !ok {
		t.Error("IP still locked after lockout expired")
	}
}

func TestLoginThrottle_LocksAccountAcrossIPs(t *testing.T) {
	lt, _ := newTestThrottle(t, 0, 3)
	for i, ip := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		if _, ok := lt.Check(ip, "Admin@Example.test "); !ok {
			t.Fatalf("attempt %d refused before limit", i+1)
		}
		lt.Failure(ip, "Admin@Example.test ")
	}
	if _, ok := lt.Check("198.51.100.4", "admin@example.test"); ok {
		t.Fatal("account not locked after 3 failures from different IPs (case/space-insensitive)")
	}
	if _, ok := lt.Check("198.51.100.4", "other@example.test"); !ok {
		t.Error("other account refused")
	}
}

func TestLoginThrottle_WindowResets(t *testing.T) {
	lt, clk := newTestThrottle(t, 3, 0)
	lt.Failure("198.51.100.7", "")
	lt.Failure("198.51.100.7", "")
	clk.advance(16 * time.Minute)
	lt.Failure("198.51.100.7", "")
	if _, ok := lt.Check("198.51.100.7", ""); !ok {
		t.Error("failures from an expired window still counted")
	}
}

func TestLoginThrottle_SuccessClearsAccountNotIP(t *testing.T) {
	lt, _ := newTestThrottle(t, 3, 3)
	lt.Failure("198.51.100.7", "a@example.test")
	lt.Failure("198.51.100.7", "a@example.test")
	lt.Success("a@example.test")
	lt.Failure("198.51.100.7", "a@example.test")
	// account count reset to 1, IP count is 3 -> IP locked
	if _, ok := lt.Check("198.51.100.9", "a@example.test"); !ok {
		t.Error("account locked although success cleared it")
	}
	if _, ok := lt.Check("198.51.100.7", "b@example.test"); ok {
		t.Error("IP not locked; success must not clear the IP counter")
	}
}

func TestLoginThrottle_FailuresDuringLockoutDoNotExtend(t *testing.T) {
	lt, clk := newTestThrottle(t, 2, 0)
	lt.Failure("198.51.100.7", "")
	lt.Failure("198.51.100.7", "")
	clk.advance(10 * time.Minute)
	lt.Failure("198.51.100.7", "")
	retry, ok := lt.Check("198.51.100.7", "")
	if ok || retry != 5*time.Minute {
		t.Errorf("Check = (%v, %v), want (5m, false)", retry, ok)
	}
}
