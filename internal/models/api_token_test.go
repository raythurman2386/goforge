package models

import (
	"reflect"
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
