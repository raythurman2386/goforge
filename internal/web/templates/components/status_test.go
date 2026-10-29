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
		{"deployment", "running", "bg-green-50 text-green-700", "Running", false},
		{"deployment", "failed", "bg-red-50 text-red-700", "Failed", false},
		{"deployment", "building", "bg-amber-50 text-amber-800", "Building", true},
		{"deployment", "rolled_back", "bg-gray-50 text-gray-700", "Rolled Back", false},
		{"container", "exited", "bg-red-50 text-red-700", "Exited", false},
		{"service", "stopped", "bg-gray-50 text-gray-700", "Stopped", false},
		{"service", "", "bg-gray-50 text-gray-700", "Unknown", false},
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

func TestRenderedAlertEscapesMessage(t *testing.T) {
	var buf bytes.Buffer
	if err := AlertMessage("error", `<img src=x>`).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "<img") || !strings.Contains(got, "&lt;img") {
		t.Fatalf("alert did not escape message: %s", got)
	}
	if !strings.Contains(got, `role="alert"`) {
		t.Fatalf("error alert missing role=alert: %s", got)
	}
}
