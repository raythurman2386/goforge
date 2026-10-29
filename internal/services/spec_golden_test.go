package services

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

var updateSpecGolden = flag.Bool("update-spec-golden", false, "rewrite testdata/spec_golden.json from the current code")

// specGoldenPath holds the container options every repo template rendered to
// on main before published ports existed (48e498e). Regenerate it only from
// a commit whose behaviour is the reference.
const specGoldenPath = "testdata/spec_golden.json"

// specSnapshot is the comparable part of docker.ContainerOptions.
type specSnapshot struct {
	Image         string            `json:"image"`
	Env           []string          `json:"env"`
	Labels        map[string]string `json:"labels"`
	Binds         []string          `json:"binds"`
	NetworkName   string            `json:"network"`
	RestartPolicy string            `json:"restart"`
	Memory        int64             `json:"memory"`
	NanoCPUs      int64             `json:"nano_cpus"`
	HealthCheck   []string          `json:"health_check"`
}

// goldenConfig is a deterministic config for tmpl: every field at its default,
// generated fields set to a fixed value.
func goldenConfig(tmpl *ServiceTemplate, domain string) map[string]interface{} {
	config := map[string]interface{}{"name": tmpl.Name + "-golden"}
	if domain != "" {
		config["domain"] = domain
	}
	for _, f := range tmpl.Config {
		switch {
		case f.Generate:
			config[f.Name] = "fixed-" + f.Name
		case f.Default != nil:
			config[f.Name] = fmt.Sprintf("%v", f.Default)
		}
	}
	return config
}

func renderSnapshots(t *testing.T) map[string]specSnapshot {
	t.Helper()
	c := loadRepoCatalog(t)
	svc := NewInstanceService(nil, nil, nil, c, "goforge_net", true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	out := make(map[string]specSnapshot)
	for _, tmpl := range c.ListTemplates() {
		for _, domain := range []string{"", tmpl.Name + "-app.example.com"} {
			config := goldenConfig(tmpl, domain)
			if err := normalizeConfig(tmpl, config); err != nil {
				t.Fatalf("%s: normalizeConfig: %v", tmpl.Name, err)
			}
			inst := &models.ServiceInstance{ID: "00000000-0000-0000-0000-000000000042"}
			inst.Version.String, inst.Version.Valid = tmpl.DefaultVersion, true
			inst.Domain.String, inst.Domain.Valid = domain, domain != ""
			spec, err := svc.buildContainerSpec(tmpl, inst, config)
			if err != nil {
				t.Fatalf("%s: buildContainerSpec: %v", tmpl.Name, err)
			}
			env := make([]string, 0, len(spec.opts.Env))
			for _, kv := range spec.opts.Env {
				// argon2id output has a random salt.
				if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(v, "$argon2id$") {
					kv = k + "=<argon2id>"
				}
				env = append(env, kv)
			}
			sort.Strings(env)
			snap := specSnapshot{
				Image: spec.opts.Image, Env: env, Labels: spec.opts.Labels, Binds: spec.opts.Binds,
				NetworkName: spec.opts.NetworkName, RestartPolicy: spec.opts.RestartPolicy,
				Memory: spec.opts.Memory, NanoCPUs: spec.opts.NanoCPUs,
			}
			if spec.opts.HealthCheck != nil {
				snap.HealthCheck = spec.opts.HealthCheck.Test
			}
			key := tmpl.Name
			if domain != "" {
				key += "+domain"
			}
			out[key] = snap
		}
	}
	return out
}

// TestBuildContainerSpec_MatchesMain checks that every template renders the
// same container options as before published ports existed. Gitea, which now
// publishes SSH, must keep identical labels; its env differs only by the SSH
// settings.
func TestBuildContainerSpec_MatchesMain(t *testing.T) {
	got := renderSnapshots(t)
	if *updateSpecGolden {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Dir(specGoldenPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(specGoldenPath, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(specGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]specSnapshot
	if err = json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Errorf("rendered %d template variants, golden has %d", len(got), len(want))
	}
	for key, w := range want {
		g, ok := got[key]
		if !ok {
			t.Errorf("%s: missing", key)
			continue
		}
		if !strings.HasPrefix(key, "gitea") {
			if gj, wj := mustJSON(t, g), mustJSON(t, w); gj != wj {
				t.Errorf("%s: container options changed\n got: %s\nwant: %s", key, gj, wj)
			}
			continue
		}
		if gj, wj := mustJSON(t, g.Labels), mustJSON(t, w.Labels); gj != wj {
			t.Errorf("%s: labels changed\n got: %s\nwant: %s", key, gj, wj)
		}
		gEnv, wEnv := withoutSSHEnv(g.Env), withoutSSHEnv(w.Env)
		if gj, wj := mustJSON(t, gEnv), mustJSON(t, wEnv); gj != wj {
			t.Errorf("%s: non-SSH env changed\n got: %s\nwant: %s", key, gj, wj)
		}
		g.Env, w.Env, g.Labels, w.Labels = nil, nil, nil, nil
		if gj, wj := mustJSON(t, g), mustJSON(t, w); gj != wj {
			t.Errorf("%s: container options changed\n got: %s\nwant: %s", key, gj, wj)
		}
	}
}

func withoutSSHEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "GITEA__SERVER__SSH_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
