package settings

import "github.com/a-h/templ"

// ActivityFilters is the activity page's filter form state.
type ActivityFilters struct {
	Source  string
	Token   string
	Action  string
	AuditID string
	// All lists every user's rows (admins only).
	All     bool
	IsAdmin bool
	// OlderURL and NewerURL page through results; empty when there is no
	// page in that direction.
	OlderURL templ.SafeURL
	NewerURL templ.SafeURL
}
