package ui

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestClass(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{"empty", nil, ""},
		{"order kept", []string{"btn", "btn-sm"}, "btn btn-sm"},
		{"blank parts dropped", []string{"", "  btn  ", ""}, "btn"},
		{"duplicates dropped", []string{"btn px-4", "px-4 text-sm", "btn"}, "btn px-4 text-sm"},
		{"multi-space input", []string{"a\tb\n c"}, "a b c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Class(tt.parts...); got != tt.want {
				t.Errorf("Class(%q) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}

func TestAttrsMergesClassAndOverridesOthers(t *testing.T) {
	base := templ.Attributes{"class": "sr-only", "id": "a", "role": "note"}
	extra := templ.Attributes{"class": "text-sm sr-only", "id": "b", "hx-get": "/x"}
	got := Attrs(base, nil, extra)

	if got["class"] != "sr-only text-sm" {
		t.Errorf("class = %q, want %q", got["class"], "sr-only text-sm")
	}
	if got["id"] != "b" {
		t.Errorf("id = %v, want caller value b", got["id"])
	}
	if got["hx-get"] != "/x" || got["role"] != "note" {
		t.Errorf("passthrough lost: %v", got)
	}
	if want := []string{"class", "hx-get", "id", "role"}; !reflect.DeepEqual(Keys(got), want) {
		t.Errorf("keys = %v, want %v", Keys(got), want)
	}
	if base["class"] != "sr-only" || base["id"] != "a" {
		t.Errorf("Attrs modified its input: %v", base)
	}
}

func TestAttrsDropsEmptyClass(t *testing.T) {
	got := Attrs(templ.Attributes{"class": ""}, templ.Attributes{"data-x": "1"})
	if _, ok := got["class"]; ok {
		t.Errorf("empty class kept: %v", got)
	}
}

func TestVisuallyHiddenPassthrough(t *testing.T) {
	var buf bytes.Buffer
	child := templ.Raw("required")
	ctx := templ.WithChildren(context.Background(), child)
	attrs := templ.Attributes{"id": "req-hint", "class": "ml-1"}
	if err := VisuallyHidden(attrs).Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{`class="sr-only ml-1"`, `id="req-hint"`, ">required</span>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
