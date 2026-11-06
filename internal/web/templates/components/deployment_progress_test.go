package components

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestDeploymentProgressUsesModelStageAndElapsed(t *testing.T) {
	deployment := &models.Deployment{
		Status:    models.DeploymentStatusBuilding,
		StartedAt: sql.NullTime{Time: time.Now().Add(-90 * time.Second), Valid: true},
	}
	var buf bytes.Buffer
	if err := DeploymentProgress(deployment).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{`data-deployment-stage="building"`, "Elapsed: 1m 30s"} {
		if !strings.Contains(body, want) {
			t.Errorf("progress lacks %q: %s", want, body)
		}
	}
	if strings.Contains(body, "%") {
		t.Error("unexpected percentage")
	}
}

func TestDeploymentProgressFailureFeedback(t *testing.T) {
	deployment := &models.Deployment{Status: models.DeploymentStatusFailed, ErrorMessage: sql.NullString{String: "build failed: fixture", Valid: true}}
	var buf bytes.Buffer
	if err := DeploymentProgress(deployment).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `role="alert"`) || !strings.Contains(buf.String(), "build failed: fixture") {
		t.Fatalf("failure feedback not visible: %s", buf.String())
	}
}

func TestFormatDeploymentElapsed(t *testing.T) {
	for _, tc := range []struct {
		duration time.Duration
		want     string
	}{{0, "0s"}, {59 * time.Second, "59s"}, {90 * time.Second, "1m 30s"}, {time.Hour + 2*time.Minute, "1h 2m"}} {
		if got := formatDeploymentElapsed(tc.duration); got != tc.want {
			t.Errorf("formatDeploymentElapsed(%s) = %q, want %q", tc.duration, got, tc.want)
		}
	}
	if got := nonnegativeDuration(-time.Second); got != 0 {
		t.Errorf("negative duration = %s, want zero", got)
	}
}
