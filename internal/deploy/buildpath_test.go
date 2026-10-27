package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBuildSpec(t *testing.T) {
	spec, err := ParseBuildSpec("", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Dockerfile != defaultDockerfile || spec.Context != "." {
		t.Fatalf("spec = %#v", spec)
	}

	spec, err = ParseBuildSpec("./docker/Dockerfile.prod", "app")
	if err != nil {
		t.Fatal(err)
	}
	if spec.Dockerfile != "docker/Dockerfile.prod" || spec.Context != "app" {
		t.Fatalf("spec = %#v", spec)
	}

	cases := []string{"../etc/passwd", "/etc/passwd", `app\Dockerfile`, "foo/../../etc/passwd"}
	for _, raw := range cases {
		if _, err := ParseBuildSpec(raw, "."); err == nil {
			t.Errorf("ParseBuildSpec(%q) accepted an escaping path", raw)
		}
		if _, err := ParseBuildSpec(defaultDockerfile, raw); err == nil {
			t.Errorf("ParseBuildSpec context %q accepted an escaping path", raw)
		}
	}
}

func TestBuildSpec_Locate(t *testing.T) {
	root := t.TempDir()
	dockerfile := filepath.Join(root, "app", "docker", "Dockerfile.prod")
	if err := os.MkdirAll(filepath.Dir(dockerfile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app", "hello.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	spec, err := ParseBuildSpec("docker/Dockerfile.prod", "app")
	if err != nil {
		t.Fatal(err)
	}
	contextDir, rel, err := spec.Locate(root)
	if err != nil {
		t.Fatal(err)
	}
	if contextDir != filepath.Join(root, "app") {
		t.Fatalf("context = %s", contextDir)
	}
	if rel != "docker/Dockerfile.prod" {
		t.Fatalf("dockerfile = %s", rel)
	}

	missing, err := ParseBuildSpec("Dockerfile.missing", "app")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := missing.Locate(root); err == nil {
		t.Fatal("missing dockerfile was accepted")
	}
}
