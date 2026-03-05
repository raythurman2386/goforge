package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

func TestUserRepository_Create(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	user := &models.User{
		Email:     uniqueEmail("create"),
		AvatarURL: sql.NullString{String: "https://example.com/avatar.png", Valid: true},
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if user.ID == "" {
		t.Error("Create() did not set ID")
	}
	if user.CreatedAt.IsZero() {
		t.Error("Create() did not set CreatedAt")
	}
	if user.UpdatedAt.IsZero() {
		t.Error("Create() did not set UpdatedAt")
	}
}

func TestUserRepository_Create_WithID(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	expectedID := "12345678-1234-5678-1234-567812345678"
	user := &models.User{
		ID:    expectedID,
		Email: uniqueEmail("custom-id"),
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if user.ID != expectedID {
		t.Errorf("Create() ID = %q, want %q", user.ID, expectedID)
	}

	// Cleanup: delete the user to avoid conflict on re-run
	_ = repo.Delete(ctx, user.ID)
}

func TestUserRepository_Create_DuplicateEmail(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	email := uniqueEmail("duplicate")
	user1 := &models.User{Email: email}
	err := repo.Create(ctx, user1)
	if err != nil {
		t.Fatalf("Create() first user error = %v", err)
	}

	user2 := &models.User{Email: email}
	err = repo.Create(ctx, user2)
	if err == nil {
		t.Error("Create() expected error for duplicate email, got nil")
	}
}

func TestUserRepository_GetByID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	if err != ErrNotFound {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByEmail_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	_, err := repo.GetByEmail(ctx, "nonexistent-"+uniqueID()+"@example.com")
	if err != ErrNotFound {
		t.Errorf("GetByEmail() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_GetByGitHubID_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	_, err := repo.GetByGitHubID(ctx, 99999999)
	if err != ErrNotFound {
		t.Errorf("GetByGitHubID() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Update_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	user := &models.User{
		ID:    "00000000-0000-0000-0000-000000000001",
		Email: uniqueEmail("update-notfound"),
	}

	err := repo.Update(ctx, user)
	if err != ErrNotFound {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Delete(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("delete")}
	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err = repo.Delete(ctx, user.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify deletion
	_, err = repo.GetByID(ctx, user.ID)
	if err != ErrNotFound {
		t.Errorf("GetByID() after delete error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_Delete_NotFound(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	err := repo.Delete(ctx, "00000000-0000-0000-0000-000000000002")
	if err != ErrNotFound {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepository_ContextCancellation(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	user := &models.User{Email: uniqueEmail("cancelled")}
	err := repo.Create(ctx, user)
	if err == nil {
		t.Error("Create() with cancelled context expected error, got nil")
	}
}

func TestUserRepository_ContextTimeout(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Give time for the timeout to expire
	time.Sleep(10 * time.Millisecond)

	user := &models.User{Email: uniqueEmail("timeout")}
	err := repo.Create(ctx, user)
	if err == nil {
		t.Error("Create() with expired context expected error, got nil")
	}
}

func TestNewUserRepository(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	if repo == nil {
		t.Fatal("NewUserRepository() returned nil")
	}
}

// TestUserRepository_EncryptionRoundTrip tests that tokens are encrypted on storage and decrypted on retrieval
func TestUserRepository_EncryptionRoundTrip(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	// Create encryption service
	cfg := &config.Config{
		EncryptionKey: "dGVzdC1rZXktZm9yLXRlc3RpbmctcHVycG9zZXMtMDE=", // 32 bytes base64 encoded
	}
	secretService, err := secrets.NewEncryptionService(cfg)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	repo := NewUserRepository(db, secretService)
	ctx := context.Background()

	originalToken := "ghp_testtoken123456789"
	user := &models.User{
		Email:             uniqueEmail("encryption"),
		GitHubAccessToken: sql.NullString{String: originalToken, Valid: true},
	}

	// Create user - token should be encrypted
	err = repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Retrieve user - token should be decrypted back to original
	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if !retrieved.GitHubAccessToken.Valid {
		t.Error("GitHubAccessToken should be valid")
	}

	if retrieved.GitHubAccessToken.String != originalToken {
		t.Errorf("Token mismatch: got %q, want %q", retrieved.GitHubAccessToken.String, originalToken)
	}

	// Also test GetByEmail
	byEmail, err := repo.GetByEmail(ctx, user.Email)
	if err != nil {
		t.Fatalf("GetByEmail() error = %v", err)
	}

	if byEmail.GitHubAccessToken.String != originalToken {
		t.Errorf("GetByEmail token mismatch: got %q, want %q", byEmail.GitHubAccessToken.String, originalToken)
	}
}

// TestUserRepository_PlaintextFallback tests that plaintext tokens (from old versions) still work
func TestUserRepository_PlaintextFallback(t *testing.T) {
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

	ctx := context.Background()
	//nolint:gosec // This is a test token, not a real credential
	plaintextToken := "ghp_plaintext_token"

	// Insert user with plaintext token directly into DB (simulating old data)
	// Use unique ID to avoid conflicts with other test runs
	userID := uuid.New().String()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, email, github_access_token, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`,
		userID, uniqueEmail("plaintext"), plaintextToken)
	if err != nil {
		t.Fatalf("Failed to insert plaintext token user: %v", err)
	}

	// Now retrieve with encryption service - should return plaintext as-is since decryption will fail
	repo := NewUserRepository(db, secretService)
	user, err := repo.GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	if user.GitHubAccessToken.String != plaintextToken {
		t.Errorf("Expected plaintext token to remain unchanged, got %q", user.GitHubAccessToken.String)
	}
}

// TestUserRepository_UpdateEncryption tests that updates also encrypt tokens
func TestUserRepository_UpdateEncryption(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	cfg := &config.Config{
		EncryptionKey: "dGVzdC1rZXktZm9yLXRlc3RpbmctcHVycG9zZXMtMDE=",
	}
	secretService, err := secrets.NewEncryptionService(cfg)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	repo := NewUserRepository(db, secretService)
	ctx := context.Background()

	// Create user without token
	user := &models.User{
		Email: uniqueEmail("update-encryption"),
	}
	err = repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Update with token
	//nolint:gosec // This is a test token, not a real credential
	newToken := "ghp_updated_token"
	user.GitHubAccessToken = sql.NullString{String: newToken, Valid: true}
	err = repo.Update(ctx, user)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// Retrieve and verify token is correct
	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID() after update error = %v", err)
	}

	if retrieved.GitHubAccessToken.String != newToken {
		t.Errorf("Updated token mismatch: got %q, want %q", retrieved.GitHubAccessToken.String, newToken)
	}
}

// TestUserRepository_NilEncryptionService tests that nil encryption service doesn't break anything
func TestUserRepository_NilEncryptionService(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewUserRepository(db, nil)
	ctx := context.Background()

	//nolint:gosec // This is a test token, not a real credential
	token := "ghp_not_encrypted"
	user := &models.User{
		Email:             uniqueEmail("nil-encryption"),
		GitHubAccessToken: sql.NullString{String: token, Valid: true},
	}

	err := repo.Create(ctx, user)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	retrieved, err := repo.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}

	// With nil encryption service, token should be stored as plaintext
	if retrieved.GitHubAccessToken.String != token {
		t.Errorf("Token should remain plaintext with nil service: got %q, want %q", retrieved.GitHubAccessToken.String, token)
	}
}
