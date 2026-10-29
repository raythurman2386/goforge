package models

import (
	"database/sql"
	"slices"
	"strings"
	"time"
)

// API token scopes. ScopeFull is the legacy scope that every token created
// before scopes existed was backfilled to. It keeps working on the HTML/HTMX
// routes and grants read and write on /api/v1, but never protected.
const (
	ScopeFull      = "full"
	ScopeRead      = "read"
	ScopeWrite     = "write"
	ScopeProtected = "protected"
)

// AllAPITokenScopes lists every scope the api_tokens.scopes column accepts.
var AllAPITokenScopes = []string{ScopeFull, ScopeRead, ScopeWrite, ScopeProtected}

// APIToken represents a personal access token.
type APIToken struct {
	ID         string
	UserID     string
	Name       string
	TokenHash  string
	Scopes     []string
	LastUsedAt sql.NullTime
	ExpiresAt  sql.NullTime
	CreatedAt  time.Time
}

// IsExpired returns true if the token has expired.
func (t *APIToken) IsExpired() bool {
	if !t.ExpiresAt.Valid {
		return false
	}
	return time.Now().After(t.ExpiresAt.Time)
}

// IsActive returns true if the token is active (not expired).
func (t *APIToken) IsActive() bool {
	return !t.IsExpired()
}

// IsFull reports whether the token carries the legacy full scope. A token
// with no stored scopes is treated as full, matching the migration default.
func (t *APIToken) IsFull() bool {
	return len(t.Scopes) == 0 || slices.Contains(t.Scopes, ScopeFull)
}

// HasScope reports whether the token grants scope on /api/v1.
//
// read and write are implied by full, and write implies read. protected is
// never implied: it has to be granted explicitly.
func (t *APIToken) HasScope(scope string) bool {
	switch scope {
	case ScopeRead:
		return t.IsFull() || slices.Contains(t.Scopes, ScopeRead) || slices.Contains(t.Scopes, ScopeWrite)
	case ScopeWrite:
		return t.IsFull() || slices.Contains(t.Scopes, ScopeWrite)
	case ScopeProtected:
		return slices.Contains(t.Scopes, ScopeProtected)
	case ScopeFull:
		return t.IsFull()
	default:
		return false
	}
}

// NormalizeScopes validates, de-duplicates, and orders requested scopes. An
// empty request means the legacy full scope. ok is false when any scope is
// unknown.
func NormalizeScopes(requested []string) (scopes []string, ok bool) {
	seen := make(map[string]bool, len(requested))
	for _, s := range requested {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if !slices.Contains(AllAPITokenScopes, s) {
			return nil, false
		}
		seen[s] = true
	}
	if len(seen) == 0 {
		return []string{ScopeFull}, true
	}
	for _, s := range AllAPITokenScopes {
		if seen[s] {
			scopes = append(scopes, s)
		}
	}
	return scopes, true
}
