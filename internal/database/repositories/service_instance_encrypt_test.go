package repositories

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestServiceInstanceRepository_EncryptsConfigAndConnectionInfo(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	enc := getTestSecretService(t)
	ctx := context.Background()

	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("svc-enc")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	repo := NewServiceInstanceRepository(db).WithEncryptor(enc)
	instance := &models.ServiceInstance{
		UserID:         user.ID,
		TemplateName:   "postgres",
		Name:           "db-" + uniqueID(),
		Status:         models.ServiceStatusRunning,
		Config:         json.RawMessage(`{"password":"super-secret"}`),
		ConnectionInfo: json.RawMessage(`{"internal":"postgres://postgres:super-secret@db/app"}`),
	}
	if err := repo.Create(ctx, instance); err != nil {
		t.Fatal(err)
	}
	if string(instance.Config) != `{"password":"super-secret"}` {
		t.Fatalf("in-memory config changed: %s", instance.Config)
	}

	var storedConfig, storedConn string
	if err := db.QueryRowContext(ctx, `SELECT config::text, connection_info::text FROM service_instances WHERE id = $1`, instance.ID).Scan(&storedConfig, &storedConn); err != nil {
		t.Fatal(err)
	}
	if containsPlain(storedConfig, "super-secret") || containsPlain(storedConn, "super-secret") {
		t.Fatalf("plaintext stored: config=%s connection=%s", storedConfig, storedConn)
	}

	got, err := repo.GetByID(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Config) != `{"password":"super-secret"}` {
		t.Fatalf("config = %s", got.Config)
	}
	if string(got.ConnectionInfo) != `{"internal":"postgres://postgres:super-secret@db/app"}` {
		t.Fatalf("connection = %s", got.ConnectionInfo)
	}

	projectRepo := NewProjectRepository(db)
	project := &models.Project{
		UserID: user.ID, Name: "app-" + uniqueID(), RepositoryURL: "https://example.com/app.git",
		DefaultBranch: "main",
	}
	if createErr := projectRepo.Create(ctx, project); createErr != nil {
		t.Fatal(createErr)
	}
	links := NewProjectServiceInstanceRepository(db).WithEncryptor(enc)
	if linkErr := links.Link(ctx, project.ID, instance.ID); linkErr != nil {
		t.Fatal(linkErr)
	}
	linked, err := links.ListByProjectID(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || string(linked[0].ConnectionInfo) != string(instance.ConnectionInfo) {
		t.Fatalf("linked connection = %#v", linked)
	}
}

func containsPlain(stored, secret string) bool {
	for i := 0; i+len(secret) <= len(stored); i++ {
		if stored[i:i+len(secret)] == secret {
			return true
		}
	}
	return false
}
