// Package ui is the GoForge templ component kit (Path A): accessible
// primitives on Tailwind 3.4 tokens, native <dialog> and popover, and the
// delegated runtime in static/js/ui.js. See docs/development/frontend.md (UI kit).
//
// Rules every component in this package follows:
//
//   - Class strings are complete literals. Variant maps return whole strings
//     from funcs whose names end in Class (scripts/class_check.py reads their
//     `return "..."` lines) or pass literals to Class(...), so Tailwind and the
//     class checker both see every token.
//   - Callers pass extra attributes (hx-*, data-*, aria-*, id) as
//     templ.Attributes. Attrs merges them over the component's defaults; a
//     caller "class" is appended to the component class, never a replacement.
//   - Overlays render into body or the top layer, never inside a region that
//     htmx polls or swaps over SSE.
package ui

import (
	"sort"
	"strings"

	"github.com/a-h/templ"
)

// Class joins class lists in order, dropping empty parts and repeated
// tokens. Pass literal strings only (or the result of a *Class helper):
//
//	ui.Class("btn", buttonSizeClass(size), extra)
//
// It does not resolve Tailwind conflicts (px-2 and px-4 both stay); the
// later utility does not win by position. Variant helpers must not emit
// conflicting utilities.
func Class(parts ...string) string {
	seen := make(map[string]struct{})
	var b strings.Builder
	for _, part := range parts {
		for _, tok := range strings.Fields(part) {
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(tok)
		}
	}
	return b.String()
}

// Attrs merges attribute sets left to right into a new map. Later sets win,
// except "class", which is merged with Class so a caller adds to the
// component's classes instead of replacing them. Nil sets are skipped and no
// input map is modified.
func Attrs(sets ...templ.Attributes) templ.Attributes {
	out := templ.Attributes{}
	for _, set := range sets {
		for k, v := range set {
			if k == "class" {
				out[k] = Class(classValue(out[k]), classValue(v))
				continue
			}
			out[k] = v
		}
	}
	if c, ok := out["class"]; ok && classValue(c) == "" {
		delete(out, "class")
	}
	return out
}

// Keys returns the attribute names of a in sorted order. Tests use it to
// assert passthrough without depending on map order.
func Keys(a templ.Attributes) []string {
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func classValue(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return c
	case templ.SafeCSS:
		return string(c)
	case interface{ String() string }:
		return c.String()
	default:
		return ""
	}
}
