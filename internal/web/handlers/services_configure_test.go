package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/services"
)

func renderConfigure(t *testing.T, cloudflareProxyMode bool) string {
	t.Helper()
	dir := t.TempDir()
	tmpl := "name: vaultwarden\ndisplay_name: Vaultwarden\nweb: true\ndocker:\n  image: vaultwarden/server:latest\n"
	if err := os.WriteFile(filepath.Join(dir, "vaultwarden.yaml"), []byte(tmpl), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog := services.NewCatalog(dir)
	if err := catalog.LoadTemplates(); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	is := services.NewInstanceService(nil, nil, nil, catalog, "goforge_net", cloudflareProxyMode, logger)
	h := NewServiceHandler(catalog, is, logger)

	req := httptest.NewRequest(http.MethodGet, "/services/vaultwarden/configure", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("template", "vaultwarden")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	h.Configure(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	return rr.Body.String()
}

func TestServiceHandler_Configure_DomainDefaults(t *testing.T) {
	t.Run("standard mode keeps .localhost default", func(t *testing.T) {
		body := renderConfigure(t, false)
		if !strings.Contains(body, `value="vaultwarden.localhost"`) {
			t.Error("domain not prefilled with vaultwarden.localhost")
		}
		if strings.Contains(body, "cfargotunnel.com") {
			t.Error("Cloudflare tunnel help shown outside proxy mode")
		}
	})

	t.Run("Cloudflare proxy mode leaves domain empty with guidance", func(t *testing.T) {
		body := renderConfigure(t, true)
		if strings.Contains(body, "vaultwarden.localhost") {
			t.Error("proxy mode still suggests a .localhost domain")
		}
		for _, want := range []string{
			`id="domain" name="domain" type="text" value=""`,
			`placeholder="vaultwarden-app.example.com"`,
			"first-level host",
			"*.apps.example.com",
			"proxied CNAME",
			"&lt;tunnel-id&gt;.cfargotunnel.com",
			"docs/configuration/cloudflare.md#service-domains",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("configure page missing %q", want)
			}
		}
	})
}
