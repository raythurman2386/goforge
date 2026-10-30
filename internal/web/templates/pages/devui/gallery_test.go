package devui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGalleryRenders(t *testing.T) {
	var buf bytes.Buffer
	if err := Gallery("tok").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{
		"<h1", "UI kit gallery", `<main id="main"`,
		`aria-labelledby="tokens-title"`, "--gf-background", "bg-primary-500",
		`class="btn-primary"`, `aria-label="Refresh"`,
		`aria-describedby="gallery-name-help"`, `class="sr-only"`,
		`<th scope="col" class="table-th">`,
		`src="/static/js/ui.js"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("gallery missing %q", want)
		}
	}
}

func TestSwatchClassCoversEveryToken(t *testing.T) {
	for _, name := range colorTokens {
		if swatchClass(name) == "" {
			t.Errorf("swatchClass(%q) is empty", name)
		}
	}
	for _, step := range primarySteps {
		if primarySwatchClass(step) == "" {
			t.Errorf("primarySwatchClass(%q) is empty", step)
		}
	}
}
