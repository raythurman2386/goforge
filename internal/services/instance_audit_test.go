package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

type memRunStore struct {
	mu   sync.Mutex
	rows []*models.AuditLog
}

func (m *memRunStore) Create(_ context.Context, l *models.AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, l)
	return nil
}

func (m *memRunStore) GetByID(_ context.Context, id string) (*models.AuditLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, nil
}

// waitFor polls until a row with action exists.
func (m *memRunStore) waitFor(t *testing.T, action string) *models.AuditLog {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, r := range m.rows {
			if r.Action == action {
				m.mu.Unlock()
				return r
			}
		}
		m.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s row", action)
	return nil
}

const uiOriginID = "0b5c7c8e-1a2b-4c3d-9e8f-a1b2c3d4e5f6"

func uiOrigin() *models.AuditLog {
	d, _ := json.Marshal(map[string]any{"via": "ui", "name": "vw"})
	return &models.AuditLog{
		ID: uiOriginID, Action: models.AuditActionServiceUpdateImage,
		UserID:    sql.NullString{String: "11111111-2222-3333-4444-555555555555", Valid: true},
		UserAgent: sql.NullString{String: "Mozilla/5.0 test", Valid: true},
		Details:   d,
	}
}

func TestUpdateInstanceImage_AuditOutcomeFailedLinksOrigin(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	store := &memRunStore{rows: []*models.AuditLog{uiOrigin()}}
	h.svc.WithAudit(store)
	inst := h.deploy(t)

	h.mu.Lock()
	h.pullErr = errors.New("registry down")
	h.mu.Unlock()

	ctx := models.WithAuditID(context.Background(), uiOriginID)
	if _, err := h.svc.UpdateInstanceImage(ctx, inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	row := store.waitFor(t, "service.run.failed")
	d, _ := row.GetDetails()
	if d["audit_id"] != uiOriginID || d["origin_action"] != models.AuditActionServiceUpdateImage || d["via"] != "ui" {
		t.Fatalf("details = %v", d)
	}
	if msg, _ := d["error"].(string); !strings.Contains(msg, "registry down") {
		t.Errorf("details.error = %q, want the pull failure", msg)
	}
	if row.ResourceType != models.ResourceTypeServiceInstance || row.UserID != uiOrigin().UserID {
		t.Errorf("row = %+v", row)
	}
	if row.Source() != models.AuditSourceUI {
		t.Errorf("source = %s, want ui", row.Source())
	}
}

func TestUpdateInstanceImage_AuditOutcomeCompleted(t *testing.T) {
	t.Parallel()
	h := newEditHarness(t)
	store := &memRunStore{rows: []*models.AuditLog{uiOrigin()}}
	h.svc.WithAudit(store)
	inst := h.deploy(t)

	ctx := models.WithAuditID(context.Background(), uiOriginID)
	if _, err := h.svc.UpdateInstanceImage(ctx, inst.ID); err != nil {
		t.Fatalf("UpdateInstanceImage: %v", err)
	}
	row := store.waitFor(t, "service.run.completed")
	d, _ := row.GetDetails()
	if d["audit_id"] != uiOriginID || d["status"] != string(models.ServiceStatusRunning) {
		t.Fatalf("details = %v", d)
	}
}

func TestRecordRunOutcome_NoAuditIDWritesNothing(t *testing.T) {
	store := &memRunStore{}
	RecordRunOutcome(store, nil, "", RunOutcome{Run: models.AuditRunService, Result: models.AuditRunFailed})
	if len(store.rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(store.rows))
	}
}

func TestRecordRunOutcome_CapsErrorAndKeepsNonUUIDRef(t *testing.T) {
	store := &memRunStore{}
	RecordRunOutcome(store, nil, uiOriginID, RunOutcome{
		Run: models.AuditRunDeployment, ResourceType: models.ResourceTypeDeployment, ResourceID: "dep-1",
		Result: models.AuditRunFailed, Error: strings.Repeat("x", 2000),
	})
	if len(store.rows) != 1 {
		t.Fatalf("rows = %d", len(store.rows))
	}
	r := store.rows[0]
	d, _ := r.GetDetails()
	if r.ResourceID.Valid || d["resource_ref"] != "dep-1" {
		t.Errorf("resource_id = %+v resource_ref = %v", r.ResourceID, d["resource_ref"])
	}
	if msg, _ := d["error"].(string); len(msg) != maxRunErrorLen {
		t.Errorf("error len = %d, want %d", len(msg), maxRunErrorLen)
	}
	if r.Action != "deployment.run.failed" {
		t.Errorf("action = %s", r.Action)
	}
}

// A Docker id is not a UUID: it goes to details.resource_ref, and the
// caller's details map is left alone.
func TestBuildAuditEntry_NonUUIDResourceGoesToResourceRef(t *testing.T) {
	in := map[string]interface{}{"via": "ui"}
	e, err := buildAuditEntry("", "u1", models.AuditActionContainerStop, models.ResourceTypeContainer, "abc123def456", in, "", "")
	if err != nil {
		t.Fatal(err)
	}
	d, _ := e.GetDetails()
	if e.ResourceID.Valid || d["resource_ref"] != "abc123def456" || d["via"] != "ui" {
		t.Fatalf("resource_id = %+v details = %v", e.ResourceID, d)
	}
	if _, ok := in["resource_ref"]; ok {
		t.Fatal("caller's details map was modified")
	}
	e, _ = buildAuditEntry("", "u1", models.AuditActionServiceDelete, models.ResourceTypeServiceInstance, uiOriginID, nil, "", "")
	if !e.ResourceID.Valid || e.ResourceID.String != uiOriginID || e.Details != nil {
		t.Fatalf("uuid resource: %+v", e)
	}
}
