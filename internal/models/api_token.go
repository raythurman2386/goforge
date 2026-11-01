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
	ID        string
	UserID    string
	Name      string
	TokenHash string
	// TokenHint is a non-secret label such as "gf_…a1b2" (prefix and last
	// four characters). Empty for tokens created before migration 021.
	TokenHint string
	Scopes    []string
	// ProjectIDs and ServiceIDs restrict the token to those projects and
	// service instances (migration 027). Both empty means host-wide: any
	// project or service the owner has. See IsRestricted.
	ProjectIDs []string
	ServiceIDs []string
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

// IsMCPCapable reports whether the token may connect to /mcp: it must carry
// explicit scopes (not the legacy full scope) and grant at least read.
func (t *APIToken) IsMCPCapable() bool {
	return !t.IsFull() && t.HasScope(ScopeRead)
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

// IsRestricted reports whether the token is limited to listed projects
// and/or services. A token with both lists empty is host-wide.
func (t *APIToken) IsRestricted() bool {
	return len(t.ProjectIDs) > 0 || len(t.ServiceIDs) > 0
}

// AllowsProject reports whether the token may touch project id (and the
// project's environments, deployments, and containers). A host-wide token
// allows every project; a restricted one only those in ProjectIDs. Scopes
// and ownership are checked separately.
func (t *APIToken) AllowsProject(id string) bool {
	return !t.IsRestricted() || containsID(t.ProjectIDs, id)
}

// AllowsService reports whether the token may touch service instance id. A
// host-wide token allows every service; a restricted one only those in
// ServiceIDs. Scopes, the protected list, and ownership are checked
// separately.
func (t *APIToken) AllowsService(id string) bool {
	return !t.IsRestricted() || containsID(t.ServiceIDs, id)
}

// containsID compares UUIDs case-insensitively; the database returns them
// lower-case and a caller may not.
func containsID(ids []string, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, v := range ids {
		if strings.EqualFold(v, id) {
			return true
		}
	}
	return false
}

// NormalizeResourceIDs trims, lower-cases, and de-duplicates resource IDs,
// keeping their order. ok is false when any value is not a UUID.
func NormalizeResourceIDs(requested []string) (ids []string, ok bool) {
	seen := make(map[string]bool, len(requested))
	ids = []string{}
	for _, v := range requested {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if !isUUID(v) {
			return nil, false
		}
		if !seen[v] {
			seen[v] = true
			ids = append(ids, v)
		}
	}
	return ids, true
}

// isUUID reports whether s is a lower-case 8-4-4-4-12 hex UUID.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return false
			}
		}
	}
	return true
}
