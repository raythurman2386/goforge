package services

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeTemplate(t *testing.T, dir, file, name, displayName string) {
	t.Helper()
	body := "name: " + name + "\ndisplay_name: " + displayName + "\n"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCatalog_LoadTemplates_LogsCountAndDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTemplate(t, dir, "a.yaml", "a", "A")
	writeTemplate(t, dir, "b.yaml", "b", "B")

	var buf bytes.Buffer
	c := NewCatalog(dir)
	c.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	if err := c.LoadTemplates(); err != nil {
		t.Fatal(err)
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.SplitN(buf.Bytes(), []byte("\n"), 2)[0], &rec); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	if rec["msg"] != "loaded service templates" || rec["level"] != "INFO" {
		t.Errorf("log = %v, want INFO loaded service templates", rec)
	}
	if rec["count"] != float64(2) {
		t.Errorf("count = %v, want 2", rec["count"])
	}
	if rec["dir"] != dir {
		t.Errorf("dir = %v, want %s", rec["dir"], dir)
	}
}

func TestCatalog_ReloadIfChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTemplate(t, dir, "redis.yaml", "redis", "Redis")

	c := NewCatalog(dir)
	if err := c.LoadTemplates(); err != nil {
		t.Fatal(err)
	}

	reload := func(step string, want bool) {
		t.Helper()
		got, err := c.ReloadIfChanged()
		if err != nil {
			t.Fatalf("%s: ReloadIfChanged error: %v", step, err)
		}
		if got != want {
			t.Fatalf("%s: reloaded = %v, want %v", step, got, want)
		}
	}

	reload("unchanged", false)

	writeTemplate(t, dir, "foo.yaml", "foo", "Foo")
	reload("added", true)
	if _, err := c.GetTemplate("foo"); err != nil {
		t.Fatalf("new template not served after reload: %v", err)
	}
	reload("unchanged after add", false)

	writeTemplate(t, dir, "foo.yaml", "foo", "Foo Renamed")
	reload("edited", true)
	if tmpl, _ := c.GetTemplate("foo"); tmpl == nil || tmpl.DisplayName != "Foo Renamed" {
		t.Fatalf("edited template not picked up: %+v", tmpl)
	}

	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	reload("non-yaml file", false)

	if err := os.Remove(filepath.Join(dir, "foo.yaml")); err != nil {
		t.Fatal(err)
	}
	reload("removed", true)
	if _, err := c.GetTemplate("foo"); err == nil {
		t.Fatal("removed template still served")
	}
	if c.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", c.Count())
	}
}

func TestCatalog_ReloadKeepsValidTemplatesWhenOneIsBroken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTemplate(t, dir, "redis.yaml", "redis", "Redis")

	c := NewCatalog(dir)
	if err := c.LoadTemplates(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("name: \"x\n  bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded, err := c.ReloadIfChanged()
	if !reloaded || err == nil || !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("reloaded=%v err=%v, want reload with broken.yaml error", reloaded, err)
	}
	if _, err := c.GetTemplate("redis"); err != nil {
		t.Fatalf("valid template dropped because another file is broken: %v", err)
	}

	// The broken file is not re-parsed on every request.
	if again, err := c.ReloadIfChanged(); again || err != nil {
		t.Fatalf("second ReloadIfChanged = %v, %v; want false, nil", again, err)
	}
}

func TestCatalog_ConcurrentReadsDuringReload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTemplate(t, dir, "redis.yaml", "redis", "Redis")
	c := NewCatalog(dir)
	if err := c.LoadTemplates(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 50 {
				if i == 0 && j%10 == 0 {
					body := "name: redis\ndisplay_name: " + strings.Repeat("R", j+1) + "\n"
					if err := os.WriteFile(filepath.Join(dir, "redis.yaml"), []byte(body), 0o600); err != nil {
						t.Error(err)
						return
					}
					_, _ = c.ReloadIfChanged()
					continue
				}
				_ = c.ListTemplates()
				_, _ = c.GetTemplate("redis")
			}
		}()
	}
	wg.Wait()
	if _, err := c.GetTemplate("redis"); err != nil {
		t.Fatal(err)
	}
}
