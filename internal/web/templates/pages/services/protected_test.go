package pages_services //nolint:revive,stylecheck // matches the templ package name

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderProtected(t *testing.T, p Protection) string {
	t.Helper()
	var buf bytes.Buffer
	if err := ProtectedCard(p).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestProtectedCardStates(t *testing.T) {
	off := renderProtected(t, Protection{InstanceID: "inst-1"})
	for _, want := range []string{`hx-post="/services/inst-1/protected"`, `name="protected"`, `value="true"`, `value="false"`, "Not protected"} {
		if !strings.Contains(off, want) {
			t.Errorf("off card missing %q", want)
		}
	}
	if strings.Contains(off, " checked") || strings.Contains(off, " disabled") {
		t.Errorf("off card should be unchecked and enabled")
	}

	on := renderProtected(t, Protection{InstanceID: "inst-1", Stored: true})
	if !strings.Contains(on, " checked") || strings.Contains(on, " disabled") {
		t.Errorf("stored card should be checked and enabled")
	}

	env := renderProtected(t, Protection{InstanceID: "inst-1", Env: true})
	if !strings.Contains(env, " checked") || !strings.Contains(env, " disabled") || !strings.Contains(env, "GOFORGE_PROTECTED_SERVICES") {
		t.Errorf("env card should be checked, disabled, and name the env var")
	}
}

// Delete opens a typed-name dialog that lives outside the polled region,
// not an hx-confirm on the button.
func TestDetailDeleteUsesTypedNameDialog(t *testing.T) {
	html := renderDetail(t, "running", nil, nil)
	if strings.Contains(html, "Are you sure you want to delete this service") {
		t.Error("delete still uses the plain hx-confirm")
	}
	for _, want := range []string{`data-ui-dialog-open="delete-service-dialog"`, `id="delete-service-dialog"`, `name="confirm_name"`, `hx-delete="/services/inst-1"`, "verify-pg", `id="service-protected"`} {
		if !strings.Contains(html, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	content := strings.Index(html, `id="service-detail-content"`)
	dialog := strings.Index(html, `id="delete-service-dialog"`)
	start, end := innerRange(t, html, `id="service-detail-content"`)
	if content < 0 || (dialog > start && dialog < end) {
		t.Error("delete dialog must be outside #service-detail-content")
	}
}
