package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/services"
)

func TestServiceHandler_Configure_PicksUpNewTemplateWithoutRestart(t *testing.T) {
	dir := t.TempDir()
	catalog := services.NewCatalog(dir)
	if err := catalog.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	is := services.NewInstanceService(nil, nil, nil, catalog, "goforge_net", false, logger)
	h := NewServiceHandler(catalog, is, logger)

	configure := func() int {
		req := httptest.NewRequest(http.MethodGet, "/services/foo/configure", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("template", "foo")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		h.Configure(rr, req)
		return rr.Code
	}

	if code := configure(); code != http.StatusNotFound {
		t.Fatalf("before adding foo.yaml: status = %d, want 404", code)
	}

	tmpl := "name: foo\ndisplay_name: Foo\ncategory: web\ndefault_version: latest\nversions: [latest]\n"
	if err := os.WriteFile(filepath.Join(dir, "foo.yaml"), []byte(tmpl), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := configure(); code != http.StatusOK {
		t.Fatalf("after adding foo.yaml: status = %d, want 200", code)
	}
}
