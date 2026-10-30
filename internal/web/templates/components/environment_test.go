package components

import (
	"os"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestEnvironmentSwitcherIsAKitDropdown(t *testing.T) {
	envs := []*models.Environment{{ID: "e1", Name: "production"}, {ID: "e2", Name: "staging"}}
	got := renderComp(t, EnvironmentSwitcher("p1", envs, "e2"))
	for _, want := range []string{
		`id="env-menu-trigger"`, `popovertarget="env-menu"`, `aria-haspopup="menu"`, `aria-expanded="false"`,
		`aria-controls="env-menu"`, `aria-label="Environment: staging"`,
		`id="env-menu"`, `popover="auto"`, `role="menu"`, `aria-labelledby="env-menu-trigger"`,
		`href="/projects/p1?env=e1"`, `href="/projects/p1?env=e2"`, `role="menuitem"`, `role="separator"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("switcher lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, `aria-current="page"`) != 1 || !strings.Contains(got, `aria-current="page" class=`) {
		t.Errorf("exactly the current environment should carry aria-current:\n%s", got)
	}
	for _, bad := range []string{`aria-expanded="true"`, `id="menu-button"`, "onclick", `class="hidden`} {
		if strings.Contains(got, bad) {
			t.Errorf("switcher still has %q", bad)
		}
	}

	if name := currentEnvName(envs, ""); name != "production" {
		t.Errorf("default env label = %q", name)
	}
	if name := currentEnvName(nil, ""); name != "No Environments" {
		t.Errorf("empty env label = %q", name)
	}
}

// The legacy hand-built menu must not come back.
func TestEnvironmentTemplNoInlineMenu(t *testing.T) {
	src, err := os.ReadFile("environment.templ")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"onclick", `"menu-button"`, `"aria-expanded": "true"`} {
		if strings.Contains(string(src), bad) {
			t.Errorf("environment.templ contains %q", bad)
		}
	}
}
