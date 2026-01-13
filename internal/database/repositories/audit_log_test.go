package repositories

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/raythurman2386/gocool/internal/models"
)

func setupAuditLogTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_time_format=sqlite")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create audit_logs table
	_, err = db.Exec(`
		CREATE TABLE audit_logs (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			action TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT,
			details TEXT DEFAULT '{}',
			ip_address TEXT,
			user_agent TEXT,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create audit_logs table: %v", err)
	}

	return db
}

func TestAuditLogRepository_Create(t *testing.T) {
	db := setupAuditLogTestDB(t)
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
	db := setupAuditLogTestDB(t)
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
