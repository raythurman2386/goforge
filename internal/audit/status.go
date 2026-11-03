package audit

import (
	"fmt"
	"sync"
	"time"
)

// IntegrityView is the last chain check, for the activity page.
type IntegrityView struct {
	Checked bool
	OK      bool
	Seq     int64
	At      time.Time
}

// Line is the admin integrity sentence. An unchecked chain says the check
// has not run yet.
func (v IntegrityView) Line() string {
	if !v.Checked {
		return "Integrity: check pending"
	}
	if !v.OK {
		return fmt.Sprintf("Integrity check failed at #%d", v.Seq)
	}
	return fmt.Sprintf("Integrity: verified through #%d at %s", v.Seq, v.At.UTC().Format("2006-01-02 15:04:05 UTC"))
}

type integrityState struct {
	mu   sync.RWMutex
	view IntegrityView
}

func (s *integrityState) set(v IntegrityView) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.view = v
	s.mu.Unlock()
}

// Integrity returns the last verify result. The zero value says the check
// has not run in this process.
func (c *Chain) Integrity() IntegrityView {
	if c == nil || c.status == nil {
		return IntegrityView{}
	}
	c.status.mu.RLock()
	defer c.status.mu.RUnlock()
	return c.status.view
}
