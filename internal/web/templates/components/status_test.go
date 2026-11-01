package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestStatusBadge(t *testing.T) {
	tests := []struct {
		kind, status string
		wantClass    string
		wantLabel    string
		wantSpinner  bool
	}{
		{"deployment", "running", "bg-success/10 text-success-foreground", "Running", false},
		{"deployment", "failed", "bg-danger/10 text-danger", "Failed", false},
		{"deployment", "building", "bg-warning/10 text-warning-foreground", "Building", true},
		{"deployment", "rolled_back", "bg-muted/50 text-foreground-secondary", "Rolled Back", false},
		{"container", "exited", "bg-danger/10 text-danger", "Exited", false},
		{"service", "stopped", "bg-muted/50 text-foreground-secondary", "Stopped", false},
		{"service", "", "bg-muted/50 text-foreground-secondary", "Unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.kind+"/"+tt.status, func(t *testing.T) {
			var buf bytes.Buffer
			if err := StatusBadge(tt.kind, tt.status).Render(context.Background(), &buf); err != nil {
				t.Fatal(err)
			}
			got := buf.String()
			if !strings.Contains(got, tt.wantClass) {
				t.Errorf("badge missing %q:\n%s", tt.wantClass, got)
			}
			if !strings.Contains(got, tt.wantLabel) {
				t.Errorf("badge missing label %q:\n%s", tt.wantLabel, got)
			}
			if spin := strings.Contains(got, "animate-spin"); spin != tt.wantSpinner {
				t.Errorf("spinner = %v, want %v", spin, tt.wantSpinner)
			}
		})
	}
}
