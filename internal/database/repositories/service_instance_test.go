package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestServiceInstanceRepository_Create(t *testing.T) {
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	// Create a user first to satisfy FK constraint
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "service-instance-test@example.com"}
	_ = userRepo.Create(ctx, user)

	s := &models.ServiceInstance{
		UserID:       user.ID,
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
	db := setupSharedTestDB(t)
	defer db.Close()

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	// Create a user first to satisfy FK constraint
	userRepo := NewUserRepository(db)
	user := &models.User{Email: "service-list-test@example.com"}
	_ = userRepo.Create(ctx, user)

	s := &models.ServiceInstance{
		UserID:       user.ID,
		TemplateName: "postgres",
		Name:         "my-db",
		Status:       models.ServiceStatusRunning,
	}
	_ = repo.Create(ctx, s)

	list, err := repo.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}

	if len(list) != 1 {
		t.Errorf("ListByUserID() len = %d, want 1", len(list))
	}
}
