package ui

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func TestLabelRequiredMarkHasScreenReaderText(t *testing.T) {
	got := render(t, Label(LabelProps{For: "x-name", Required: true}), "Name")
	assertHas(t, got, `<label for="x-name" class="label">Name`, `aria-hidden="true">*</span>`, `<span class="sr-only">(required)</span>`)
	got = render(t, Label(LabelProps{For: "x-name"}), "Name")
	assertLacks(t, got, "(required)", "*")
}

func TestInputFieldWiring(t *testing.T) {
	tests := []struct {
		name  string
		p     InputProps
		wants []string
		nots  []string
	}{
		{"id defaults to field- prefix, never the bare name", InputProps{Name: "email", Label: "Email"},
			[]string{`<label for="field-email" class="label">Email</label>`, `<input id="field-email" name="email" type="text" value="" class="input">`},
			[]string{`id="email"`, "aria-describedby", "aria-invalid", "required"}},
		{"help is described", InputProps{ID: "login-email", Name: "email", Type: "email", Label: "Email", Help: "Your login."},
			[]string{`aria-describedby="login-email-help"`, `<p id="login-email-help" class="mt-1.5 text-xs text-muted-foreground">Your login.</p>`},
			[]string{"aria-invalid"}},
		{"error is described and invalid", InputProps{ID: "p", Name: "port", Label: "Port", Help: "h", Error: "Too big."},
			[]string{`aria-describedby="p-help p-error"`, `aria-invalid="true"`, `<p id="p-error" class="mt-1.5 text-xs font-medium text-red-600 dark:text-red-400">Too big.</p>`},
			nil},
		{"extra describedby ids come last", InputProps{ID: "k", Name: "key", Label: "Key", Help: "h", DescribedBy: "k-state"},
			[]string{`aria-describedby="k-help k-state"`}, nil},
		{"autocomplete placeholder required disabled", InputProps{ID: "pw", Name: "password", Type: "password", Label: "Password", Autocomplete: "current-password", Placeholder: "secret", Required: true, Disabled: true},
			[]string{`type="password"`, `placeholder="secret"`, `autocomplete="current-password"`, " required", " disabled", "(required)"}, nil},
		{"attrs and control class pass through", InputProps{ID: "n", Name: "n", Label: "N", ControlClass: "w-auto", Attrs: templ.Attributes{"min": "1", "hx-get": "/x"}},
			[]string{`class="input w-auto"`, `min="1"`, `hx-get="/x"`}, nil},
		{"wrapper class", InputProps{ID: "w", Name: "w", Label: "W", Class: "mb-4"},
			[]string{`<div class="mb-4"><label for="w"`}, nil},
		{"no label renders the bare control and messages", InputProps{ID: "b", Name: "b", Error: "Bad."},
			[]string{`<input id="b"`, `aria-describedby="b-error"`, `id="b-error"`}, []string{"<label", "<div"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(t, Input(tt.p), "")
			assertHas(t, got, tt.wants...)
			assertLacks(t, got, tt.nots...)
		})
	}
}

func TestTextareaFieldWiring(t *testing.T) {
	got := render(t, Textarea(TextareaProps{ID: "notes", Name: "notes", Label: "Notes", Value: "a<b", Help: "h", Error: "e", Required: true}), "")
	assertHas(t, got, `<label for="notes" class="label">Notes`, `<textarea id="notes" name="notes" rows="4" required aria-describedby="notes-help notes-error" aria-invalid="true" class="input">a&lt;b</textarea>`)
	got = render(t, Textarea(TextareaProps{Name: "body", Label: "Body", Rows: 8}), "")
	assertHas(t, got, `id="field-body"`, `rows="8"`)
}

func TestSelectOptionsBlankAndWiring(t *testing.T) {
	opts := []SelectOption{{Value: "debug"}, {Value: "info", Label: "Info"}, {Value: "trace", Disabled: true}}
	got := render(t, Select(SelectProps{ID: "lvl", Name: "log_level", Label: "Level", Options: opts, Selected: "info", Help: "h", Required: true}), "")
	assertHas(t, got, `<label for="lvl" class="label">Level`, `<select id="lvl" name="log_level" required aria-describedby="lvl-help" class="select">`,
		`<option value="debug">debug</option>`, `<option value="info" selected>Info</option>`, `<option value="trace" disabled>trace</option>`)
	assertLacks(t, got, `<option value="">`)

	got = render(t, Select(SelectProps{Name: "lvl", Label: "Level", Options: StringOptions([]string{"a", "b"}), Blank: true, BlankLabel: "Default"}), "")
	assertHas(t, got, `<select id="field-lvl"`, `<option value="" selected>Default</option>`, `<option value="a">a</option>`)

	got = render(t, Select(SelectProps{Name: "lvl", Options: StringOptions([]string{"a"}), Blank: true, Selected: "a", Error: "Pick one."}), "")
	assertHas(t, got, `<option value="">`, `<option value="a" selected>a</option>`, `aria-invalid="true"`, `aria-describedby="field-lvl-error"`)
	assertLacks(t, got, `<option value="" selected>`, "<label")
}

func TestCheckboxVariants(t *testing.T) {
	t.Run("plain posts on, label is linked", func(t *testing.T) {
		got := render(t, Checkbox(CheckboxProps{ID: "login-remember", Name: "remember_me", Label: "Remember me"}), "")
		assertHas(t, got, `<input type="checkbox" id="login-remember" name="remember_me" class="checkbox">`, `<label for="login-remember" class="font-medium text-label">Remember me</label>`)
		assertLacks(t, got, "value=", `type="hidden"`, "aria-describedby", `role="switch"`)
	})
	t.Run("hidden false comes after the box", func(t *testing.T) {
		got := render(t, Checkbox(CheckboxProps{ID: "svc-x", Name: "x", Value: "true", Label: "X", Checked: true, HiddenFalse: true}), "")
		box := strings.Index(got, `<input type="checkbox" id="svc-x" name="x" value="true" checked class="checkbox">`)
		hidden := strings.Index(got, `<input type="hidden" name="x" value="false">`)
		if box < 0 || hidden < 0 || hidden < box {
			t.Fatalf("want checkbox then hidden false, got:\n%s", got)
		}
	})
	t.Run("description is described", func(t *testing.T) {
		got := render(t, Checkbox(CheckboxProps{ID: "sec", Name: "is_secret", Label: "Secret", Description: "Masked."}), "")
		assertHas(t, got, `aria-describedby="sec-description"`, `<p id="sec-description" class="text-xs text-muted-foreground">Masked.</p>`)
	})
	t.Run("ids from name and value never equal the name", func(t *testing.T) {
		got := render(t, Checkbox(CheckboxProps{Name: "scopes", Value: "read", Label: "read"}), "")
		assertHas(t, got, `id="field-scopes-read"`, `for="field-scopes-read"`)
	})
	t.Run("children label", func(t *testing.T) {
		got := render(t, Checkbox(CheckboxProps{ID: "c", Name: "c"}), "Run <code>x</code>")
		assertHas(t, got, `<label for="c" class="font-medium text-label">Run <code>x</code>`)
	})
	t.Run("bare control", func(t *testing.T) {
		got := render(t, CheckboxControl(CheckboxProps{ID: "svc-1", Name: "service_ids", Value: "1", Disabled: true}), "")
		assertHas(t, got, `<input type="checkbox" id="svc-1" name="service_ids" value="1" disabled class="checkbox">`)
		assertLacks(t, got, "<label")
	})
}

func TestSwitchIsANativeCheckboxWithRoleSwitch(t *testing.T) {
	got := render(t, Switch(CheckboxProps{ID: "backup-enabled", Name: "enabled", Value: "true", Label: "Enabled", Description: "Stops the schedule.", Checked: true}), "")
	assertHas(t, got,
		`<input type="checkbox" role="switch" id="backup-enabled" name="enabled" value="true" checked aria-describedby="backup-enabled-description" class="switch">`,
		`<label for="backup-enabled" class="font-medium text-label">Enabled</label>`,
		`id="backup-enabled-description"`)
	assertLacks(t, got, "aria-checked", `type="hidden"`)
}

func TestCheckboxGroupIsAFieldset(t *testing.T) {
	got := render(t, CheckboxGroup(CheckboxGroupProps{Legend: "Scopes", DescribedBy: "scopes-help", Class: "mb-4"}), `<p id="scopes-help">h</p>`)
	assertHas(t, got, `<fieldset aria-describedby="scopes-help" class="mb-4">`, `<legend class="label">Scopes`, `<p id="scopes-help">h</p>`, `</fieldset>`)
}

func TestFormClassHelpers(t *testing.T) {
	if got := LabelClass("mb-0"); got != "label mb-0" {
		t.Errorf("LabelClass = %q", got)
	}
	if got := inputClass(""); got != "input" {
		t.Errorf("inputClass = %q", got)
	}
	if got := selectClass("w-auto"); got != "select w-auto" {
		t.Errorf("selectClass = %q", got)
	}
	if checkboxClass() != "checkbox" || switchClass() != "switch" {
		t.Error("checkbox or switch class changed")
	}
	if ControlID("", "name") == "name" {
		t.Error("ControlID fell back to the bare name")
	}
}

func TestCopyAttrs(t *testing.T) {
	got := render(t, Button(ButtonProps{Attrs: CopyAttrs("snippet")}), "Copy")
	assertHas(t, got, `data-ui-copy="snippet"`, `type="button"`)
	assertLacks(t, got, "onclick")
}
