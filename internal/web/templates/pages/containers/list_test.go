package containers

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/models"
)

// The Type column used to render nothing: it was a templ {{ }} statement,
// which discards the value, instead of a { } expression.
func TestContainerRowShowsType(t *testing.T) {
	tracked := []*models.Container{
		{DockerID: "dep", DeploymentID: sql.NullString{String: "d1", Valid: true}},
		{DockerID: "svc", ServiceInstanceID: sql.NullString{String: "s1", Valid: true}},
	}
	tests := []struct{ id, want string }{
		{"dep", "Deployment"},
		{"svc", "Service"},
		{"other", "External"},
	}
	for _, tt := range tests {
		var buf bytes.Buffer
		dc := container.Summary{ID: tt.id, Names: []string{"/app-" + tt.id}, Image: "nginx:latest", State: "running"}
		if err := ContainerRow(dc, tracked).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		got := buf.String()
		if !strings.Contains(got, tt.want) {
			t.Errorf("row for %s missing type %q:\n%s", tt.id, tt.want, got)
		}
		if !strings.Contains(got, "app-"+tt.id) || strings.Contains(got, ">/app-") {
			t.Errorf("row for %s should show the name without a leading slash", tt.id)
		}
		if !strings.Contains(got, `aria-label="Stop app-`+tt.id+`"`) {
			t.Errorf("row for %s missing labeled stop button", tt.id)
		}
	}
}
