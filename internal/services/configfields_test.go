package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestCatalog_ParsesBoolAndSelectFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string]string{
		"good.yaml": `name: good
config:
  - name: signups
    type: bool
    default: true
  - name: level
    type: select
    options: [info, debug]
    default: debug
`,
		"no-options.yaml": `name: no-options
config:
  - name: level
    type: select
`,
		"bad-select-default.yaml": `name: bad-select-default
config:
  - name: level
    type: select
    options: [info]
    default: trace
`,
		"bad-bool-default.yaml": `name: bad-bool-default
config:
  - name: signups
    type: bool
    default: "yes"
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCatalog(dir)
	err := c.LoadTemplates()
	if err == nil {
		t.Fatal("LoadTemplates() error = nil, want errors for invalid templates")
	}
	for _, want := range []string{"select needs options", `default "trace" is not one of`, "default yes is not true or false"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("load error %q missing %q", err, want)
		}
	}
	if c.Count() != 1 {
		t.Fatalf("Count() = %d, want only the valid template", c.Count())
	}
	tmpl, err := c.GetTemplate("good")
	if err != nil {
		t.Fatal(err)
	}
	if f := tmpl.Config[0]; f.Type != FieldTypeBool || !f.BoolDefault() {
		t.Errorf("bool field = %+v", f)
	}
	if f := tmpl.Config[1]; f.Type != FieldTypeSelect || len(f.Options) != 2 || f.Options[1] != "debug" || f.Default != "debug" {
		t.Errorf("select field = %+v", f)
	}
}

func TestNormalizeConfig(t *testing.T) {
	t.Parallel()
	tmpl := &ServiceTemplate{Config: []ConfigField{
		{Name: "on", Label: "Enabled", Type: FieldTypeBool, Default: true},
		{Name: "off", Type: FieldTypeBool},
		{Name: "level", Label: "Log Level", Type: FieldTypeSelect, Options: []string{"info", "debug"}, Default: "info"},
		{Name: "mode", Label: "Mode", Type: FieldTypeSelect, Options: []string{"a", "b"}, Required: true},
		{Name: "note", Type: FieldTypeText},
	}}
	tests := []struct {
		name    string
		in      map[string]interface{}
		want    map[string]interface{}
		wantErr string
	}{
		{
			name: "defaults fill missing values",
			in:   map[string]interface{}{"mode": "a"},
			want: map[string]interface{}{"on": "true", "off": "false", "level": "info", "mode": "a"},
		},
		{
			name: "bools normalize",
			in:   map[string]interface{}{"on": "FALSE", "off": true, "mode": "b", "note": "ture"},
			want: map[string]interface{}{"on": "false", "off": "true", "level": "info", "mode": "b", "note": "ture"},
		},
		{
			name:    "bool typo rejected",
			in:      map[string]interface{}{"on": "ture", "mode": "a"},
			wantErr: `Enabled must be true or false, got "ture"`,
		},
		{
			name:    "select value outside options rejected",
			in:      map[string]interface{}{"level": "trace", "mode": "a"},
			wantErr: `Log Level must be one of info, debug, got "trace"`,
		},
		{
			name:    "required select missing",
			in:      map[string]interface{}{},
			wantErr: "Mode is required; choose one of a, b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := normalizeConfig(tmpl, tt.in)
			if tt.wantErr != "" {
				if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want ErrInvalidConfig containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			for k, v := range tt.want {
				if tt.in[k] != v {
					t.Errorf("%s = %#v, want %#v", k, tt.in[k], v)
				}
			}
		})
	}
}

func TestDeployInstance_RejectsInvalidBool(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	_, err := h.svc.DeployInstance(context.Background(), "user-1", "vaultwarden", map[string]interface{}{
		"name":            "vw",
		"domain":          "vw.example.com",
		"signups_allowed": "ture",
	})
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "Allow Signups must be true or false") {
		t.Fatalf("err = %v, want ErrInvalidConfig for signups_allowed", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.instance != nil || len(h.events) != 0 {
		t.Errorf("deploy went ahead: instance=%v events=%v", h.instance, h.events)
	}
}

func TestDeployInstance_NormalizesBoolIntoEnv(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	if _, err := h.svc.DeployInstance(context.Background(), "user-1", "vaultwarden", map[string]interface{}{
		"name":            "vw",
		"domain":          "vw.example.com",
		"signups_allowed": "False",
	}); err != nil {
		t.Fatalf("DeployInstance: %v", err)
	}
	h.wait(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.created) != 1 || !containsString(h.created[0].Env, "SIGNUPS_ALLOWED=false") {
		t.Fatalf("env = %v, want SIGNUPS_ALLOWED=false", h.created)
	}
}

func TestUpdateInstanceConfig_RejectsInvalidBool(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	inst := h.deploy(t)
	h.mu.Lock()
	events := len(h.events)
	h.mu.Unlock()

	_, err := h.svc.UpdateInstanceConfig(context.Background(), inst.ID, map[string]interface{}{"signups_allowed": "maybe"})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) != events {
		t.Errorf("docker touched by rejected edit: %v", h.events[events:])
	}
	if h.instance.Status != models.ServiceStatusRunning {
		t.Errorf("status = %s after rejected edit, want running", h.instance.Status)
	}
}

func TestRepoTemplates_BoolFields(t *testing.T) {
	t.Parallel()
	c := loadRepoCatalog(t)
	want := map[string][]string{
		"vaultwarden": {"signups_allowed"},
		"gitea":       {"disable_registration", "require_signin_view"},
	}
	for tmplName, fields := range want {
		tmpl, err := c.GetTemplate(tmplName)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range fields {
			found := false
			for _, f := range tmpl.Config {
				if f.Name == name {
					found = true
					if f.Type != FieldTypeBool || strings.Contains(f.Label, "true") {
						t.Errorf("%s.%s = %+v, want bool without a true/false hint", tmplName, name, f)
					}
				}
			}
			if !found {
				t.Errorf("%s missing field %s", tmplName, name)
			}
		}
	}
}
