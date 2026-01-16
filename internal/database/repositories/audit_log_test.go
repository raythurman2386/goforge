package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestAuditLogRepository_Create(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewAuditLogRepository(db)
	ctx := context.Background()

	l := &models.AuditLog{
		Action:       models.AuditActionLogin,
		ResourceType: models.ResourceTypeUser,
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
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewAuditLogRepository(db)
	ctx := context.Background()

	_ = repo.Create(ctx, &models.AuditLog{Action: "A1", ResourceType: "R1"})
	_ = repo.Create(ctx, &models.AuditLog{Action: "A2", ResourceType: "R2"})

	list, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("List() len = %d, want 2", len(list))
	}
}
