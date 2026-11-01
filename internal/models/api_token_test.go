package models

import (
	"reflect"
	"strings"
	"testing"
)

func TestAPITokenScopes(t *testing.T) {
	cases := []struct {
		scopes                 []string
		read, write, protected bool
	}{
		{nil, true, true, false},
		{[]string{ScopeFull}, true, true, false},
		{[]string{ScopeRead}, true, false, false},
		{[]string{ScopeWrite}, true, true, false},
		{[]string{ScopeWrite, ScopeProtected}, true, true, true},
		{[]string{ScopeProtected}, false, false, true},
	}
	for _, c := range cases {
		tok := &APIToken{Scopes: c.scopes}
		if tok.HasScope(ScopeRead) != c.read || tok.HasScope(ScopeWrite) != c.write || tok.HasScope(ScopeProtected) != c.protected {
			t.Errorf("%v: read=%v write=%v protected=%v", c.scopes, tok.HasScope(ScopeRead), tok.HasScope(ScopeWrite), tok.HasScope(ScopeProtected))
		}
	}
}

func TestNormalizeScopes(t *testing.T) {
	got, ok := NormalizeScopes(nil)
	if !ok || !reflect.DeepEqual(got, []string{ScopeFull}) {
		t.Errorf("empty: %v %v", got, ok)
	}
	got, ok = NormalizeScopes([]string{" Protected", "write", "write", ""})
	if !ok || !reflect.DeepEqual(got, []string{ScopeWrite, ScopeProtected}) {
		t.Errorf("mixed: %v %v", got, ok)
	}
	if _, ok := NormalizeScopes([]string{"admin"}); ok {
		t.Error("unknown scope accepted")
	}
}

func TestIsMCPCapable(t *testing.T) {
	for _, tc := range []struct {
		scopes []string
		want   bool
	}{
		{nil, false},
		{[]string{ScopeFull}, false},
		{[]string{ScopeProtected}, false},
		{[]string{ScopeWrite}, true}, // write implies read
		{[]string{ScopeRead}, true},
		{[]string{ScopeRead, ScopeWrite, ScopeProtected}, true},
	} {
		if got := (&APIToken{Scopes: tc.scopes}).IsMCPCapable(); got != tc.want {
			t.Errorf("IsMCPCapable(%v) = %v, want %v", tc.scopes, got, tc.want)
		}
	}
}

func TestAPITokenResourceScope(t *testing.T) {
	const p1, p2, s1 = "33333333-3333-3333-3333-333333333333", "33333333-3333-4333-8333-333333333332", "66666666-6666-6666-6666-666666666666"
	hostWide := &APIToken{}
	if hostWide.IsRestricted() || !hostWide.AllowsProject(p1) || !hostWide.AllowsService(s1) {
		t.Fatal("a token with no lists must be host-wide")
	}
	projOnly := &APIToken{ProjectIDs: []string{p1}}
	if !projOnly.IsRestricted() || !projOnly.AllowsProject(p1) || projOnly.AllowsProject(p2) || projOnly.AllowsService(s1) {
		t.Fatal("project-only token: wrong answers")
	}
	if !projOnly.AllowsProject(strings.ToUpper(p1)) || projOnly.AllowsProject("") {
		t.Fatal("IDs should compare case-insensitively and empty never matches")
	}
	svcOnly := &APIToken{ServiceIDs: []string{s1}}
	if !svcOnly.IsRestricted() || svcOnly.AllowsProject(p1) || !svcOnly.AllowsService(s1) {
		t.Fatal("service-only token: wrong answers")
	}
}

func TestNormalizeResourceIDs(t *testing.T) {
	got, ok := NormalizeResourceIDs([]string{" 33333333-3333-3333-3333-333333333333 ", "", "33333333-3333-3333-3333-333333333333", "66666666-6666-6666-6666-66666666666A"})
	if !ok || len(got) != 2 || got[1] != "66666666-6666-6666-6666-66666666666a" {
		t.Fatalf("got %v %v", got, ok)
	}
	if got, ok := NormalizeResourceIDs(nil); !ok || got == nil || len(got) != 0 {
		t.Fatalf("empty = %#v %v, want [] true", got, ok)
	}
	for _, bad := range []string{"x", "33333333-3333-3333-3333-33333333333", "33333333_3333-3333-3333-333333333333", "' or 1=1 --"} {
		if _, ok := NormalizeResourceIDs([]string{bad}); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}
