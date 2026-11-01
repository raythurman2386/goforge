package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/models"
)

// ListFiltered backs Settings → Activity: source, token name, action
// substring, and audit_id (a change plus its outcomes), scoped by user.
func TestAuditLogRepository_ListFiltered_Activity(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	repo := NewAuditLogRepository(db)

	users := NewUserRepository(db, nil)
	u := &models.User{Email: uniqueEmail("activity")}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	other := &models.User{Email: uniqueEmail("activity-other")}
	if err := users.Create(ctx, other); err != nil {
		t.Fatal(err)
	}
	token := "tok-" + uniqueID()
	uid := sql.NullString{String: u.ID, Valid: true}
	mk := func(user sql.NullString, action, ua string, d map[string]any) *models.AuditLog {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		l := &models.AuditLog{UserID: user, Action: action, ResourceType: models.ResourceTypeServiceInstance,
			Details: b, UserAgent: sql.NullString{String: ua, Valid: ua != ""}}
		if err := repo.Create(ctx, l); err != nil {
			t.Fatalf("create %s: %v", action, err)
		}
		return l
	}
	mcp := mk(uid, "api_v1.restart_service", "goforge-mcp/1.0", map[string]any{"via": "api_v1", "token_name": token})
	outcome := mk(uid, "api_v1.restart_service.completed", "goforge-mcp/1.0", map[string]any{"audit_id": mcp.ID, "token_name": token})
	api := mk(uid, "api_v1.deploy_service", "curl/8", map[string]any{"via": "api_v1", "token_name": token})
	ui := mk(uid, "service.delete", "Mozilla/5.0", map[string]any{"via": "ui"})
	run := mk(uid, "deployment.run.failed", "goforge-mcp/1.0", map[string]any{"via": "api_v1", "audit_id": api.ID, "token_name": token})
	foreign := mk(sql.NullString{String: other.ID, Valid: true}, "service.delete", "", map[string]any{"via": "ui"})

	ids := func(f AuditFilter) map[string]bool {
		t.Helper()
		f.Limit = 100
		logs, err := repo.ListFiltered(ctx, f)
		if err != nil {
			t.Fatalf("ListFiltered(%+v): %v", f, err)
		}
		got := map[string]bool{}
		for i, l := range logs {
			if i > 0 && l.CreatedAt.After(logs[i-1].CreatedAt) {
				t.Errorf("not newest first at %d", i)
			}
			got[l.ID] = true
		}
		return got
	}
	check := func(name string, f AuditFilter, want, notWant []*models.AuditLog) {
		t.Helper()
		got := ids(f)
		for _, l := range want {
			if !got[l.ID] {
				t.Errorf("%s: missing %s", name, l.Action)
			}
		}
		for _, l := range notWant {
			if got[l.ID] {
				t.Errorf("%s: unexpected %s", name, l.Action)
			}
		}
	}

	check("own", AuditFilter{UserID: u.ID}, []*models.AuditLog{mcp, outcome, api, ui, run}, []*models.AuditLog{foreign})
	check("mcp", AuditFilter{UserID: u.ID, Source: models.AuditSourceMCP}, []*models.AuditLog{mcp, outcome, run}, []*models.AuditLog{api, ui})
	check("api", AuditFilter{UserID: u.ID, Source: models.AuditSourceAPI}, []*models.AuditLog{api}, []*models.AuditLog{mcp, ui, run})
	check("agent", AuditFilter{UserID: u.ID, Source: models.AuditSourceAgent}, []*models.AuditLog{mcp, outcome, api, run}, []*models.AuditLog{ui})
	check("ui", AuditFilter{UserID: u.ID, Source: models.AuditSourceUI}, []*models.AuditLog{ui}, []*models.AuditLog{mcp, api, run})
	check("token", AuditFilter{TokenName: token}, []*models.AuditLog{mcp, outcome, api, run}, []*models.AuditLog{ui, foreign})
	check("contains", AuditFilter{UserID: u.ID, ActionContains: "RESTART"}, []*models.AuditLog{mcp, outcome}, []*models.AuditLog{api, ui})
	check("audit_id", AuditFilter{AuditID: api.ID}, []*models.AuditLog{api, run}, []*models.AuditLog{mcp, outcome, ui})

	page, err := repo.ListFiltered(ctx, AuditFilter{UserID: u.ID, Limit: 2})
	if err != nil || len(page) != 2 || page[0].ID != run.ID {
		t.Errorf("limit 2: %d rows, err %v", len(page), err)
	}

	if got, err := repo.GetByID(ctx, "not-a-uuid"); got != nil || err != nil {
		t.Errorf("GetByID(non-uuid) = %v, %v", got, err)
	}
	if got, err := repo.GetByID(ctx, mcp.ID); err != nil || got == nil || got.Action != mcp.Action {
		t.Errorf("GetByID = %v, %v", got, err)
	}
}

func TestQueueRepository_AuditIDRoundTrip(t *testing.T) {
	db := getTestDB(t)
	cleanupQueue(t, db)
	ctx := context.Background()
	envID := createTestEnvironment(t, db)
	dep := createTestDeployment(t, db, envID)
	repo := NewQueueRepository(db)

	auditID := uuid.New().String()
	item := &models.QueueItem{DeploymentID: dep.ID, AuditID: sql.NullString{String: auditID, Valid: true}}
	if err := repo.Enqueue(ctx, item); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByDeploymentID(ctx, dep.ID)
	if err != nil || got == nil || got.AuditID.String != auditID {
		t.Fatalf("GetByDeploymentID audit_id = %+v, %v", got, err)
	}
	next, err := repo.Dequeue(ctx)
	if err != nil || next == nil || next.AuditID.String != auditID {
		t.Fatalf("Dequeue audit_id = %+v, %v", next, err)
	}

	dep2 := createTestDeployment(t, db, envID)
	plain := &models.QueueItem{DeploymentID: dep2.ID}
	if err = repo.Enqueue(ctx, plain); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetByID(ctx, plain.ID)
	if err != nil || got == nil || got.AuditID.Valid {
		t.Fatalf("no audit id round trip = %+v, %v", got, err)
	}
}
