package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestAuditLogRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewAuditLogRepository(db)
	ctx := context.Background()

	l := &models.AuditLog{
		Action:       models.AuditActionLogin,
		ResourceType: models.ResourceTypeUser,
		// ResourceID is UUID type, leave null for simplicity
	}

	err := repo.Create(ctx, l)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if l.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestAuditLogRepository_List(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewAuditLogRepository(db)
	ctx := context.Background()

	// Create unique action names for this test to identify our entries
	action1 := "test-action-" + uniqueID()
	action2 := "test-action-" + uniqueID()

	err1 := repo.Create(ctx, &models.AuditLog{Action: action1, ResourceType: "R1"})
	err2 := repo.Create(ctx, &models.AuditLog{Action: action2, ResourceType: "R2"})
	if err1 != nil || err2 != nil {
		t.Fatalf("Create() errors: %v, %v", err1, err2)
	}

	// List returns all audit logs, we verify the call works
	list, err := repo.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Count our test entries
	var count int
	for _, log := range list {
		if log.Action == action1 || log.Action == action2 {
			count++
		}
	}

	if count != 2 {
		t.Errorf("List() found %d of our test logs, want 2", count)
	}
}

func TestAuditLogRepository_List_Pagination(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewAuditLogRepository(db)
	ctx := context.Background()

	// Test pagination with limit
	list, err := repo.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List() with limit error = %v", err)
	}

	if len(list) > 2 {
		t.Errorf("List() with limit=2 returned %d items, want <= 2", len(list))
	}
}
