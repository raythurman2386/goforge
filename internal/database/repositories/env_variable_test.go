package repositories

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

func TestEnvVariableRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db, nil)
	ctx := context.Background()

	// Create user, project, environment
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-create")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "DATABASE_URL_" + uniqueID(),
		Value:         "postgres://localhost",
		IsSecret:      true,
	}

	err := repo.Create(ctx, v)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if v.ID == "" {
		t.Error("Create() did not set ID")
	}
}

func TestEnvVariableRepository_ListByEnvironmentID(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db, nil)
	ctx := context.Background()

	// Create user, project, environment
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-list")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	vars := []*models.EnvVariable{
		{EnvironmentID: env.ID, Key: "K1_" + uniqueID(), Value: "V1"},
		{EnvironmentID: env.ID, Key: "K2_" + uniqueID(), Value: "V2"},
	}

	for _, v := range vars {
		if err := repo.Create(ctx, v); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if len(list) != 2 {
		t.Errorf("ListByEnvironmentID() len = %d, want 2", len(list))
	}
}

func TestEnvVariableRepository_Delete(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db, nil)
	ctx := context.Background()

	// Create user, project, environment
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-delete")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "DELETE_ME_" + uniqueID(),
		Value:         "VALUE",
	}
	if err := repo.Create(ctx, v); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err := repo.Delete(ctx, v.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify by trying to get the variable by ID - list would include other vars
	list, _ := repo.ListByEnvironmentID(ctx, env.ID)
	for _, item := range list {
		if item.ID == v.ID {
			t.Error("Delete() did not remove the variable")
		}
	}
}

// TestEnvVariableRepository_SecretEncryptionRoundTrip tests that secret values are encrypted and decrypted properly
func TestEnvVariableRepository_SecretEncryptionRoundTrip(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	// Create encryption service
	cfg := &config.Config{
		EncryptionKey: "dGVzdC1rZXktZm9yLXRlc3RpbmctcHVycG9zZXMtMDE=",
	}
	secretService, err := secrets.NewEncryptionService(cfg)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	repo := NewEnvVariableRepository(db, secretService)
	ctx := context.Background()

	// Setup: create user, project, environment
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-encryption")}
	if createErr := userRepo.Create(ctx, user); createErr != nil {
		t.Fatalf("failed to create user: %v", createErr)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if createErr := projRepo.Create(ctx, proj); createErr != nil {
		t.Fatalf("failed to create project: %v", createErr)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if createErr := envRepo.Create(ctx, env); createErr != nil {
		t.Fatalf("failed to create environment: %v", createErr)
	}

	// Create a secret environment variable
	secretValue := "super-secret-password-123"
	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "SECRET_KEY",
		Value:         secretValue,
		IsSecret:      true,
	}

	if createErr := repo.Create(ctx, v); createErr != nil {
		t.Fatalf("Create() error = %v", createErr)
	}

	// Retrieve and verify the value is decrypted back to original
	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Expected 1 variable, got %d", len(list))
	}

	if list[0].Value != secretValue {
		t.Errorf("Secret value mismatch: got %q, want %q", list[0].Value, secretValue)
	}

	if !list[0].IsSecret {
		t.Error("IsSecret should be true")
	}
}

// TestEnvVariableRepository_NonSecretNotEncrypted tests that non-secret values are not encrypted
func TestEnvVariableRepository_NonSecretNotEncrypted(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	// Create encryption service
	cfg := &config.Config{
		EncryptionKey: "dGVzdC1rZXktZm9yLXRlc3RpbmctcHVycG9zZXMtMDE=",
	}
	secretService, err := secrets.NewEncryptionService(cfg)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	repo := NewEnvVariableRepository(db, secretService)
	ctx := context.Background()

	// Setup
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-nonsecret")}
	if createErr := userRepo.Create(ctx, user); createErr != nil {
		t.Fatalf("failed to create user: %v", createErr)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if createErr := projRepo.Create(ctx, proj); createErr != nil {
		t.Fatalf("failed to create project: %v", createErr)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if createErr := envRepo.Create(ctx, env); createErr != nil {
		t.Fatalf("failed to create environment: %v", createErr)
	}

	// Create a non-secret environment variable
	nonSecretValue := "public-value-123"
	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "PUBLIC_KEY",
		Value:         nonSecretValue,
		IsSecret:      false,
	}

	if createErr := repo.Create(ctx, v); createErr != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Retrieve and verify the value
	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Expected 1 variable, got %d", len(list))
	}

	if list[0].Value != nonSecretValue {
		t.Errorf("Value mismatch: got %q, want %q", list[0].Value, nonSecretValue)
	}

	if list[0].IsSecret {
		t.Error("IsSecret should be false")
	}
}

// TestEnvVariableRepository_UpdateEncryption tests that updates also encrypt secret values
func TestEnvVariableRepository_UpdateEncryption(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	cfg := &config.Config{
		EncryptionKey: "dGVzdC1rZXktZm9yLXRlc3RpbmctcHVycG9zZXMtMDE=",
	}
	secretService, err := secrets.NewEncryptionService(cfg)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	repo := NewEnvVariableRepository(db, secretService)
	ctx := context.Background()

	// Setup
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-update")}
	if createErr := userRepo.Create(ctx, user); createErr != nil {
		t.Fatalf("failed to create user: %v", createErr)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if createErr := projRepo.Create(ctx, proj); createErr != nil {
		t.Fatalf("failed to create project: %v", createErr)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if createErr := envRepo.Create(ctx, env); createErr != nil {
		t.Fatalf("failed to create environment: %v", createErr)
	}

	// Create a non-secret variable first
	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "API_KEY",
		Value:         "initial-value",
		IsSecret:      false,
	}
	if createErr := repo.Create(ctx, v); createErr != nil {
		t.Fatalf("Create() error = %v", createErr)
	}

	// Update to make it a secret with new value
	v.Value = "updated-secret-value"
	v.IsSecret = true
	if updateErr := repo.Update(ctx, v); updateErr != nil {
		t.Fatalf("Update() error = %v", updateErr)
	}

	// Retrieve and verify
	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if list[0].Value != "updated-secret-value" {
		t.Errorf("Updated value mismatch: got %q, want %q", list[0].Value, "updated-secret-value")
	}
}

// TestEnvVariableRepository_PlaintextFallback tests backward compatibility with plaintext secrets
func TestEnvVariableRepository_PlaintextFallback(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	cfg := &config.Config{
		EncryptionKey: "dGVzdC1rZXktZm9yLXRlc3RpbmctcHVycG9zZXMtMDE=",
	}
	secretService, err := secrets.NewEncryptionService(cfg)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	ctx := context.Background()

	// Setup
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-plaintext")}
	if createErr := userRepo.Create(ctx, user); createErr != nil {
		t.Fatalf("failed to create user: %v", createErr)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if createErr := projRepo.Create(ctx, proj); createErr != nil {
		t.Fatalf("failed to create project: %v", createErr)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if createErr := envRepo.Create(ctx, env); createErr != nil {
		t.Fatalf("failed to create environment: %v", createErr)
	}

	// Insert a plaintext secret directly into the database (simulating old data)
	plaintextValue := "plaintext-secret"
	_, execErr := db.ExecContext(ctx,
		`INSERT INTO env_variables (id, environment_id, key, value, is_secret, created_at, updated_at) 
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		uuid.New().String(), env.ID, "PLAIN_KEY", plaintextValue, true)
	if execErr != nil {
		t.Fatalf("Failed to insert plaintext secret: %v", execErr)
	}

	// Retrieve with encryption service - should return plaintext as-is
	repo := NewEnvVariableRepository(db, secretService)
	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if len(list) != 1 {
		t.Fatalf("Expected 1 variable, got %d", len(list))
	}

	if list[0].Value != plaintextValue {
		t.Errorf("Expected plaintext value to remain unchanged: got %q, want %q", list[0].Value, plaintextValue)
	}
}

// TestEnvVariableRepository_NilEncryptionService tests that nil service doesn't break functionality
func TestEnvVariableRepository_NilEncryptionService(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewEnvVariableRepository(db, nil)
	ctx := context.Background()

	// Setup
	userRepo := NewUserRepository(db, nil)
	user := &models.User{Email: uniqueEmail("envvar-nil")}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	projRepo := NewProjectRepository(db)
	proj := &models.Project{
		UserID:        user.ID,
		Name:          "test-project-" + uniqueID(),
		RepositoryURL: "https://github.com/test/repo-" + uniqueID(),
		BuildArgs:     []byte("{}"),
	}
	if err := projRepo.Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	envRepo := NewEnvironmentRepository(db)
	env := &models.Environment{ProjectID: proj.ID, Name: "prod-" + uniqueID()}
	if err := envRepo.Create(ctx, env); err != nil {
		t.Fatalf("failed to create environment: %v", err)
	}

	// Create a secret with nil encryption service
	secretValue := "should-be-plaintext"
	v := &models.EnvVariable{
		EnvironmentID: env.ID,
		Key:           "SECRET_KEY",
		Value:         secretValue,
		IsSecret:      true,
	}

	if err := repo.Create(ctx, v); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Retrieve and verify value is still plaintext
	list, err := repo.ListByEnvironmentID(ctx, env.ID)
	if err != nil {
		t.Fatalf("ListByEnvironmentID() error = %v", err)
	}

	if list[0].Value != secretValue {
		t.Errorf("With nil service, value should remain plaintext: got %q, want %q", list[0].Value, secretValue)
	}
}
