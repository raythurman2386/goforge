package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/notify/notifytest"
)

func runQueuedNotify(ctx context.Context, t *testing.T, svc *Service, n notify.Notifier) {
	t.Helper()
	q := &memQueue{}
	w := (&Worker{service: svc, queueRepo: q, timeout: 10 * time.Second}).WithAudit(&memAudit{rows: []*models.AuditLog{mcpOrigin(t)}}).WithNotifier(n)
	if err := w.Enqueue(ctx, "dep-1"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	item, _ := q.Dequeue(ctx)
	w.processItem(item) //nolint:contextcheck // processItem runs on its own context, as in the worker loop.
}

// A failed deployment POSTs one deploy.failed with the deployment link and
// the run's audit_id; the build output's secrets are redacted.
func TestWorker_FailedDeployNotifiesOnce(t *testing.T) {
	_, _, projects, _, svc := pipelineFixture(t)
	projects.items["proj-1"].Name = "shop"
	svc.dockerClient = &fakeRuntime{buildErr: errors.New("dockerfile syntax; DATABASE_URL=postgres://app:pw-DO-NOT-LEAK@db/app")}
	n, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, "/hook", "")

	runQueuedNotify(models.WithAuditID(context.Background(), mcpOriginID), t, svc, n)
	n.Wait()

	reqs := rcv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("POSTs = %d, want 1", len(reqs))
	}
	p := reqs[0].Payload(t)
	if p.Event != notify.EventDeployFailed || p.Resource.Type != "deployment" || p.Resource.ID != "dep-1" || p.Resource.Name != "shop / production" {
		t.Errorf("payload %+v", p)
	}
	if p.AuditID != mcpOriginID {
		t.Errorf("audit_id = %q, want %s", p.AuditID, mcpOriginID)
	}
	if p.Link != "https://forge.example.test/projects/proj-1/deployments/dep-1" {
		t.Errorf("link %q", p.Link)
	}
	if want := "dockerfile syntax"; len(p.Error) == 0 || !strings.Contains(p.Error, want) || strings.Contains(reqs[0].Body, "pw-DO-NOT-LEAK") {
		t.Errorf("error %q (body must keep the cause and drop the password)", p.Error)
	}
}

func TestWorker_SucceededOrCanceledDeployDoesNotNotify(t *testing.T) {
	_, _, _, _, svc := pipelineFixture(t)
	n, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, "/hook", "")
	runQueuedNotify(context.Background(), t, svc, n)

	deploys, _, _, _, svc2 := pipelineFixture(t)
	deploys.items["dep-1"].Status = models.DeploymentStatusCanceled
	svc2.dockerClient = &fakeRuntime{buildErr: context.Canceled}
	runQueuedNotify(context.Background(), t, svc2, n)
	n.Wait()
	if got := len(rcv.Requests()); got != 0 {
		t.Fatalf("POSTs = %d, want 0", got)
	}
}
