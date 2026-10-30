package handlers

import (
	"net/http"
	"strconv"

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

// UIGalleryDialog serves the gallery's dialog examples, mounted next to
// UIGallery under GOFORGE_DEV_MODE only. ?ok=1 answers 204 (a successful
// request from inside a dialog closes it), ?fail=1 answers 422 with a
// message (shown in the dialog's error node), and otherwise it renders
// devui.DemoDialog at ?depth= (default 1), non-dismissible with
// ?nondismissible=1.
func UIGalleryDialog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	switch {
	case q.Get("ok") == "1":
		w.WriteHeader(http.StatusNoContent)
		return
	case q.Get("fail") == "1":
		http.Error(w, "Demo error: the server refused this request.", http.StatusUnprocessableEntity)
		return
	}
	depth, err := strconv.Atoi(q.Get("depth"))
	if err != nil || depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := devui.DemoDialog(depth, q.Get("nondismissible") == "1").Render(r.Context(), w); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}
