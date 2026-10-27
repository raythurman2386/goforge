package handlers

import (
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/models"
)

// requireAdmin allows the request only for an authenticated admin.
// A missing session is unauthorized. A signed-in non-admin is forbidden.
func requireAdmin(w http.ResponseWriter, r *http.Request) *models.User {
	user := auth.GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil
	}
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil
	}
	return user
}
