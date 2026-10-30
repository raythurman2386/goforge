package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// render renders c with optional children.
func render(t *testing.T, c templ.Component, children string) string {
	t.Helper()
	ctx := context.Background()
	if children != "" {
		ctx = templ.WithChildren(ctx, templ.Raw(children))
	}
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func assertHas(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func assertLacks(t *testing.T, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(got, n) {
			t.Errorf("unexpected %q in:\n%s", n, got)
		}
	}
}

func TestButtonElementAndARIA(t *testing.T) {
	tests := []struct {
		name  string
		p     ButtonProps
		wants []string
		nots  []string
	}{
		{"zero value is a type=button secondary", ButtonProps{},
			[]string{`<button type="button"`, `class="btn-secondary"`}, []string{"disabled", "<a "}},
		{"submit", ButtonProps{Type: "submit"}, []string{`type="submit"`}, nil},
		{"disabled button", ButtonProps{Variant: ButtonSecondary, Disabled: true},
			[]string{`<button type="button" disabled`, `class="btn-secondary"`}, nil},
		{"link", ButtonProps{Variant: ButtonSecondary, Href: "/projects"},
			[]string{`<a href="/projects"`, `class="btn-secondary"`}, []string{"type=", "<button"}},
		{"disabled link has no href", ButtonProps{Href: "/projects", Disabled: true},
			[]string{`<span aria-disabled="true"`}, []string{"href=", "<a "}},
		{"icon button is named by Label", ButtonProps{Variant: ButtonGhostDanger, Size: ButtonIcon, Label: "Delete web"},
			[]string{`aria-label="Delete web"`, `class="btn-ghost-danger btn-icon"`}, nil},
		{"caller class and attrs merge", ButtonProps{Variant: ButtonPrimary, Size: ButtonSM, Class: "w-full", Attrs: templ.Attributes{"hx-post": "/x", "data-modal-close": true}},
			[]string{`class="btn-primary btn-sm w-full"`, `hx-post="/x"`, " data-modal-close"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, Button(tt.p), "Go")
			assertHas(t, got, append(tt.wants, ">Go<")...)
			assertLacks(t, got, tt.nots...)
		})
	}
}

func TestButtonClassCoversEveryVariant(t *testing.T) {
	for _, v := range []ButtonVariant{ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonWarning, ButtonGhost, ButtonGhostPrimary, ButtonGhostWarning, ButtonGhostDanger, ButtonLink} {
		if got := ButtonClass(v, ButtonMD); !strings.HasPrefix(got, "btn-") {
			t.Errorf("ButtonClass(%q) = %q", v, got)
		}
	}
	if got := ButtonClass(ButtonLink, ButtonInline); got != "btn-link btn-inline" {
		t.Errorf("ButtonClass(link, inline) = %q", got)
	}
}

func TestBadgeIsTextWithTone(t *testing.T) {
	got := render(t, Badge(BadgeProps{Tone: BadgeSuccess, Attrs: templ.Attributes{"data-status": "running"}}), "Running")
	assertHas(t, got, "<span ", "bg-green-50 text-green-700", "rounded-full", `data-status="running"`, ">Running</span>")
	assertLacks(t, got, "role=")

	sq := render(t, Badge(BadgeProps{Tone: BadgePrimary, Variant: BadgeSubtle, Square: true, Size: BadgeMD}), "x")
	assertHas(t, sq, "rounded-md", "px-3 py-1 text-sm", "bg-primary-50 text-primary-700")
	assertLacks(t, sq, "ring-1")

	for _, v := range []BadgeVariant{BadgeSoft, BadgeSubtle, BadgeSolid, BadgeOutline} {
		for _, tone := range []BadgeTone{BadgeNeutral, BadgePrimary, BadgeSuccess, BadgeWarning, BadgeDanger, BadgeInfo} {
			if badgeToneClass(v, tone) == "" {
				t.Errorf("badgeToneClass(%q, %q) is empty", v, tone)
			}
		}
	}
}

func TestCardParts(t *testing.T) {
	assertHas(t, render(t, Card(CardProps{}), "x"), `<div class="card">x</div>`)
	assertHas(t, render(t, Card(CardProps{Flush: true, Class: "mb-6", Attrs: templ.Attributes{"id": "c"}}), "x"), `class="card p-0 mb-6"`, `id="c"`)
	assertHas(t, render(t, Card(CardProps{Href: "/projects/1"}), "x"), `<a href="/projects/1" class="card">`)
	for level, tag := range map[CardTitleLevel]string{0: "h3", 2: "h2", 3: "h3", 4: "h4"} {
		got := render(t, CardTitle(level, nil), "T")
		assertHas(t, got, "<"+tag+` class="card-title">T</`+tag+">")
	}
	assertHas(t, render(t, CardContent(templ.Attributes{"id": "body"}), "x"), `class="card-body"`, `id="body"`)
	assertHas(t, render(t, CardHeader(nil), "x"), `class="card-header"`)
	assertHas(t, render(t, CardFooter(nil), "x"), "border-t border-divider")
}

func TestAlertRoles(t *testing.T) {
	for tone, role := range map[AlertTone]string{AlertError: "alert", AlertWarning: "status", AlertInfo: "status", AlertSuccess: "status", "": "status"} {
		got := render(t, Alert(AlertProps{Tone: tone}), "<p>m</p>")
		assertHas(t, got, `role="`+role+`"`, `aria-hidden="true"`, "<p>m</p>")
		assertLacks(t, got, "data-ui-dismiss")
	}
	got := render(t, Alert(AlertProps{Tone: AlertWarning, Title: "Heads up", Dismissible: true}), "<p>m</p>")
	assertHas(t, got, `data-ui="alert"`, `<p class="font-semibold">Heads up</p>`,
		`<button type="button" data-ui-dismiss`, `aria-label="Dismiss"`, "h-11 w-11")
}

func TestAlertMessageEscapes(t *testing.T) {
	got := render(t, AlertMessage(AlertError, `<img src=x>`), "")
	if strings.Contains(got, "<img") || !strings.Contains(got, "&lt;img") {
		t.Fatalf("alert did not escape message: %s", got)
	}
	assertHas(t, got, `role="alert"`)
}

func TestSkeletonIsHiddenAndMotionSafe(t *testing.T) {
	for shape, cls := range map[SkeletonShape]string{SkeletonLine: "skeleton-line", SkeletonCircle: "skeleton-circle", SkeletonBlock: "skeleton-block", "": "skeleton-line"} {
		got := render(t, Skeleton(shape, "w-20"), "")
		assertHas(t, got, `aria-hidden="true"`, "motion-safe:animate-pulse", cls+" w-20")
		assertLacks(t, got, " animate-pulse", "role=")
	}
}

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"ray.thurman@example.com": "RT",
		"admin@example.com":       "A",
		"Ada Lovelace":            "AL",
		"grace brewster hopper":   "GB",
		"raythurman2386":          "R",
		"  ":                      "?",
		"@@":                      "?",
		"élan vital":              "ÉV",
	} {
		if got := Initials(in); got != want {
			t.Errorf("Initials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAvatarARIA(t *testing.T) {
	dec := render(t, Avatar(AvatarProps{Name: "ray@example.com"}), "")
	assertHas(t, dec, `aria-hidden="true"`, ">R</span>", "h-10 w-10")
	assertLacks(t, dec, "role=", "<img")

	named := render(t, Avatar(AvatarProps{Name: "Ada Lovelace", Label: "Ada Lovelace", Size: AvatarLG}), "")
	assertHas(t, named, `role="img"`, `aria-label="Ada Lovelace"`, ">AL</span>", "h-12 w-12")
	assertLacks(t, named, "aria-hidden")
}

func TestBreadcrumbARIA(t *testing.T) {
	got := render(t, Breadcrumb([]Crumb{
		{Label: "Projects", Href: "/projects"},
		{Label: "web", Href: "/projects/1"},
		{Label: "abc123", Href: "/ignored"},
	}), "")
	assertHas(t, got, `<nav class="mb-4" aria-label="Breadcrumb">`, "<ol ",
		`href="/projects"`, `href="/projects/1"`,
		`aria-current="page">abc123</span>`)
	assertLacks(t, got, `href="/ignored"`)
	if n := strings.Count(got, "<svg"); n != 2 {
		t.Errorf("separators = %d, want 2", n)
	}
	if n := strings.Count(got, `aria-current="page"`); n != 1 {
		t.Errorf("aria-current count = %d, want 1", n)
	}
	if strings.Count(got, "<svg") != strings.Count(got, `<svg class="h-4 w-4 shrink-0 text-muted-foreground/60" fill="currentColor" viewBox="0 0 20 20" aria-hidden="true">`) {
		t.Error("every separator must be aria-hidden")
	}
}
