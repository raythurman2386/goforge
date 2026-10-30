package components

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Path A PR4 gates: every dialog is a ui.Dialog and every confirmation is
// hx-confirm (the kit confirm dialog), never window.confirm() or the legacy
// #modal contract. components/ui itself is excluded (it documents them).
func TestNoLegacyModalOrInlineConfirm(t *testing.T) {
	banned := []string{`confirm(`, `id="modal"`, `data-modal`, `components.Modal(`, `@Modal(`, `#modal`, `modal-error`, `modal-title`}
	root := ".."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && filepath.Base(path) == "ui" {
			return filepath.SkipDir
		}
		if info.IsDir() || !strings.HasSuffix(path, ".templ") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range banned {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %q", path, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A button that appends a dialog fragment to body says so to assistive tech.
var bodyTriggerRe = regexp.MustCompile(`hx-target"?\s*[:=]\s*"body"[^}>]*?hx-swap"?\s*[:=]\s*"beforeend"`)

func TestDialogTriggersHaveHasPopup(t *testing.T) {
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".templ") {
			return err
		}
		b, _ := os.ReadFile(path)
		for _, loc := range bodyTriggerRe.FindAllIndex(b, -1) {
			end := loc[1] + 200
			if end > len(b) {
				end = len(b)
			}
			start := loc[0] - 300
			if start < 0 {
				start = 0
			}
			if !strings.Contains(string(b[start:end]), `aria-haspopup`) {
				t.Errorf("%s: body-appending trigger near byte %d lacks aria-haspopup=\"dialog\" (use ui.OpenDialogAttrs)", path, loc[0])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMigratedDialogs(t *testing.T) {
	for name, got := range map[string]string{
		"CreateEnvironmentModal": renderComp(t, CreateEnvironmentModal("p1", "tok")),
		"AddVariableModal":       renderComp(t, AddVariableModal("e1", "tok")),
	} {
		for _, want := range []string{` id="dialog-`, `data-ui-dialog="ephemeral"`, `aria-labelledby="dialog-`, `data-ui-dialog-error`, `data-ui-dialog-close`} {
			if !strings.Contains(got, want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
	}
	env := renderComp(t, EnvironmentSwitcher("p1", nil, ""))
	for _, want := range []string{`hx-get="/projects/p1/envs/new"`, `hx-target="body"`, `hx-swap="beforeend"`, `aria-haspopup="dialog"`} {
		if !strings.Contains(env, want) {
			t.Errorf("environment switcher New Environment item lacks %q: %s", want, env)
		}
	}
}

func renderComp(t *testing.T, c interface {
	Render(ctx context.Context, w io.Writer) error
}) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
