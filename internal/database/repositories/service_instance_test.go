package repositories

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestServiceInstanceRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	// Create a user to satisfy FK constraint
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("svc-instance-create")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	s := &models.ServiceInstance{
		UserID:       user.ID,
		TemplateName: "postgres",
		Name:         "my-db-" + uniqueID(),
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

func TestServiceInstanceRepository_GetByID(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	// Create a user to satisfy FK constraint
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("svc-instance-getbyid")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	s := &models.ServiceInstance{
		UserID:       user.ID,
		TemplateName: "postgres",
		Name:         "my-db-" + uniqueID(),
		Status:       models.ServiceStatusRunning,
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, err := repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if found.Name != s.Name {
		t.Errorf("GetByID() Name = %q, want %q", found.Name, s.Name)
	}
}

func TestServiceInstanceRepository_GetByID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestServiceInstanceRepository_ListByUserID(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	// Create a user to satisfy FK constraint
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("svc-instance-list")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	instances := []*models.ServiceInstance{
		{UserID: user.ID, TemplateName: "postgres", Name: "db-" + uniqueID(), Status: models.ServiceStatusRunning},
		{UserID: user.ID, TemplateName: "redis", Name: "cache-" + uniqueID(), Status: models.ServiceStatusRunning},
	}

	for _, s := range instances {
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	list, err := repo.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("ListByUserID() len = %d, want 2", len(list))
	}
}

func TestServiceInstanceRepository_Delete(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	// Create a user to satisfy FK constraint
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("svc-instance-delete")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	s := &models.ServiceInstance{
		UserID:       user.ID,
		TemplateName: "postgres",
		Name:         "delete-me-" + uniqueID(),
		Status:       models.ServiceStatusRunning,
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := repo.Delete(ctx, s.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.GetByID(ctx, s.ID)
	if err != ErrNotFound {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}

func TestServiceInstanceRepository_ListAll(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	userRepo := NewUserRepository(db, nil)
	templates := []string{"postgres", "gitea"}
	want := make([]string, 0, len(templates))
	for _, tmpl := range templates {
		user := &models.User{Email: uniqueEmail("svc-listall-" + tmpl)}
		if err := userRepo.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
		s := &models.ServiceInstance{UserID: user.ID, TemplateName: tmpl, Name: tmpl + "-" + uniqueID(), Status: models.ServiceStatusRunning}
		if err := repo.Create(ctx, s); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		want = append(want, s.ID)
	}

	list, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	found := map[string]*models.ServiceInstance{}
	for _, s := range list {
		found[s.ID] = s
	}
	for _, id := range want {
		s, ok := found[id]
		if !ok {
			t.Fatalf("ListAll() missing %s (instances from every user must be listed)", id)
		}
		if s.TemplateName == "" || s.Name == "" || s.Status != models.ServiceStatusRunning || s.Config != nil {
			t.Errorf("ListAll() row = %+v", s)
		}
	}
}
