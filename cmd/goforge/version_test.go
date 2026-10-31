package main

import (
	"bytes"
	"testing"
)

// A build without -X main.version (go test, go run) reports "dev"; release
// builds stamp the tag through the Makefile VERSION ldflags.
func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--version"})
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetArgs(nil) })
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--version: %v", err)
	}
	if got, want := out.String(), "goforge dev\n"; got != want {
		t.Fatalf("--version printed %q, want %q", got, want)
	}
}
