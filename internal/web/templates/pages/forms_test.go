package pages

import (
	"strings"
	"testing"
)

// Login and Register use kit fields: labels point at form-prefixed ids,
// never the bare field name, and field names are unchanged.
func TestAuthFormsUseKitFields(t *testing.T) {
	login := render(t, Login("tok", "", true))
	for _, want := range []string{
		`<label for="login-email" class="label">Email address`,
		`<input id="login-email" name="email" type="email"`,
		`autocomplete="email"`,
		`<label for="login-password" class="label">Password`,
		`autocomplete="current-password"`,
		`<input type="checkbox" id="login-remember" name="remember_me" class="checkbox">`,
		`<label for="login-remember"`,
		`<span class="sr-only">(required)</span>`,
	} {
		if !strings.Contains(login, want) {
			t.Errorf("login missing %q", want)
		}
	}
	register := render(t, Register("tok", ""))
	for _, want := range []string{
		`<input id="register-email" name="email" type="email"`,
		`<input id="register-password" name="password" type="password"`,
		`<input id="register-confirm-password" name="confirm_password" type="password"`,
		`autocomplete="new-password"`,
	} {
		if !strings.Contains(register, want) {
			t.Errorf("register missing %q", want)
		}
	}
	for _, page := range []string{login, register} {
		for _, bad := range []string{`id="email"`, `id="password"`, `id="email-address"`} {
			if strings.Contains(page, bad) {
				t.Errorf("auth page has legacy id %q", bad)
			}
		}
	}
}
