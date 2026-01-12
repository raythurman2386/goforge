package repositories

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/user/gocool/internal/models"
)

func setupServiceInstanceTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_time_format=sqlite")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Create service_instances table
	_, err = db.Exec(`
		CREATE TABLE service_instances (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			template_name TEXT NOT NULL,
			name TEXT NOT NULL,
			version TEXT,
			config TEXT DEFAULT '{}',
			domain TEXT,
			status TEXT NOT NULL,
			connection_info TEXT DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("failed to create service_instances table: %v", err)
	}

	return db
}

func TestServiceInstanceRepository_Create(t *testing.T) {
	db := setupServiceInstanceTestDB(t)
	defer db.Close()

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	s := &models.ServiceInstance{
		UserID:       "user-1",
		TemplateName: "postgres",
		Name:         "my-db",
		Status:       models.ServiceStatusPending,
	}

	err := repo.Create(ctx, s)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if s.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestServiceInstanceRepository_ListByUserID(t *testing.T) {
	db := setupServiceInstanceTestDB(t)
	defer db.Close()

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	userID := "user-1"
	s := &models.ServiceInstance{
		UserID:       userID,
		TemplateName: "postgres",
		Name:         "my-db",
		Status:       models.ServiceStatusRunning,
	}
	_ = repo.Create(ctx, s)

	list, err := repo.ListByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}

	if len(list) != 1 {
		t.Errorf("ListByUserID() len = %d, want 1", len(list))
	}
}
