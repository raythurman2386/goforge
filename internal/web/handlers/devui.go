package handlers

import (
	"net/http"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/devui"
)

// UIGallery renders the components/ui gallery. web.Router mounts it at
// /dev/ui only when GOFORGE_DEV_MODE is true; production builds answer 404.
func UIGallery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := devui.Gallery(auth.GetCSRFToken(r)).Render(r.Context(), w); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}
