package layouts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileVersion(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "output.css")
	if got := fileVersion(p); got != devVersion {
		t.Fatalf("missing file: got %q, want dev", got)
	}
	if err := os.WriteFile(p, []byte("body{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	v1 := fileVersion(p)
	if len(v1) != 12 || v1 == devVersion {
		t.Fatalf("got %q, want 12-char hash", v1)
	}
	if err := os.WriteFile(p, []byte("body{color:red}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v2 := fileVersion(p); v2 == v1 {
		t.Fatalf("hash did not change after content change: %q", v2)
	}
}
