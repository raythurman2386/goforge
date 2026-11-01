package services

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

// waitPOSTs waits for n requests at the receiver.
func waitPOSTs(t *testing.T, rcv *notifytest.Receiver, n int) []notifytest.Request {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if reqs := rcv.Requests(); len(reqs) >= n {
			return reqs
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("receiver got %d POSTs, want %d", len(rcv.Requests()), n)
	return nil
}

// A failed background image update POSTs one service.failed carrying the
// instance's last_error, link, and audit_id.
func TestUpdateInstanceImage_FailedNotifiesWithLastError(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	n, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, "/hook", "")
	store := &memRunStore{rows: []*models.AuditLog{uiOrigin()}}
	h.svc.WithAudit(store).WithNotifier(n)
	inst := h.deploy(t)

	h.mu.Lock()
	h.pullErr = errors.New("registry down")
	h.mu.Unlock()

	ctx := models.WithAuditID(context.Background(), uiOriginID)
	if _, err := h.svc.UpdateInstanceImage(ctx, inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	store.waitFor(t, "service.run.failed")
	reqs := waitPOSTs(t, rcv, 1)
	n.Wait()
	if got := len(rcv.Requests()); got != 1 {
		t.Fatalf("POSTs = %d, want 1", got)
	}
	p := reqs[0].Payload(t)
	if p.Event != notify.EventServiceFailed || p.Resource.Type != models.ResourceTypeServiceInstance || p.Resource.ID != inst.ID || p.Resource.Name != inst.Name {
		t.Errorf("payload %+v", p)
	}
	if !strings.Contains(p.Error, "registry down") || p.AuditID != uiOriginID || p.Link != "https://forge.example.test/services/"+inst.ID {
		t.Errorf("error %q audit %q link %q", p.Error, p.AuditID, p.Link)
	}
}

func TestUpdateInstanceImage_SucceededDoesNotNotify(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	n, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, "/hook", "")
	store := &memRunStore{rows: []*models.AuditLog{uiOrigin()}}
	h.svc.WithAudit(store).WithNotifier(n)
	inst := h.deploy(t)
	ctx := models.WithAuditID(context.Background(), uiOriginID)
	if _, err := h.svc.UpdateInstanceImage(ctx, inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	// The audit row is written just before the alert would be sent.
	store.waitFor(t, "service.run.completed")
	n.Wait()
	if got := len(rcv.Requests()); got != 0 {
		t.Fatalf("POSTs = %d, want 0", got)
	}
}
