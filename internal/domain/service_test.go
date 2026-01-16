package domain

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// mockRepository implements Repository for testing.
type mockRepository struct {
	domains map[string]*models.CustomDomain
	byEnv   map[string][]*models.CustomDomain
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		domains: make(map[string]*models.CustomDomain),
		byEnv:   make(map[string][]*models.CustomDomain),
	}
}

func (r *mockRepository) Create(ctx context.Context, domain *models.CustomDomain) error {
	if domain.ID == "" {
		domain.ID = "domain-" + domain.Domain
	}
	r.domains[domain.ID] = domain
	r.byEnv[domain.EnvironmentID] = append(r.byEnv[domain.EnvironmentID], domain)
	return nil
}

func (r *mockRepository) GetByID(ctx context.Context, id string) (*models.CustomDomain, error) {
	d, ok := r.domains[id]
	if !ok {
		return nil, ErrDomainNotFound
	}
	return d, nil
}

func (r *mockRepository) GetByDomain(ctx context.Context, domain string) (*models.CustomDomain, error) {
	for _, d := range r.domains {
		if d.Domain == domain {
			return d, nil
		}
	}
	return nil, ErrDomainNotFound
}

func (r *mockRepository) GetByEnvironmentID(ctx context.Context, envID string) ([]*models.CustomDomain, error) {
	return r.byEnv[envID], nil
}

func (r *mockRepository) Update(ctx context.Context, domain *models.CustomDomain) error {
	if _, ok := r.domains[domain.ID]; !ok {
		return ErrDomainNotFound
	}
	r.domains[domain.ID] = domain
	return nil
}

func (r *mockRepository) Delete(ctx context.Context, id string) error {
	d, ok := r.domains[id]
	if !ok {
		return ErrDomainNotFound
	}
	delete(r.domains, id)
	// Remove from byEnv
	envDomains := r.byEnv[d.EnvironmentID]
	for i, ed := range envDomains {
		if ed.ID == id {
			r.byEnv[d.EnvironmentID] = append(envDomains[:i], envDomains[i+1:]...)
			break
		}
	}
	return nil
}

func (r *mockRepository) ListPendingVerification(ctx context.Context) ([]*models.CustomDomain, error) {
	var result []*models.CustomDomain
	for _, d := range r.domains {
		if d.NeedsVerification() {
			result = append(result, d)
		}
	}
	return result, nil
}

func (r *mockRepository) ListExpiringSSL(ctx context.Context, before time.Time) ([]*models.CustomDomain, error) {
	var result []*models.CustomDomain
	for _, d := range r.domains {
		if d.SSLExpiresAt.Valid && d.SSLExpiresAt.Time.Before(before) {
			result = append(result, d)
		}
	}
	return result, nil
}

func TestService_AddDomain(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Test adding a valid domain
	domain, err := svc.AddDomain(ctx, "env-123", "example.com")
	if err != nil {
		t.Fatalf("AddDomain() error = %v", err)
	}

	if domain.Domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", domain.Domain)
	}
	if domain.Status != models.DomainStatusPending {
		t.Errorf("expected status pending, got %v", domain.Status)
	}
	if domain.VerificationToken == "" {
		t.Error("expected verification token to be set")
	}
	if domain.VerificationRecord == "" {
		t.Error("expected verification record to be set")
	}
	if !domain.ForceHTTPS {
		t.Error("expected ForceHTTPS to default to true")
	}
}

func TestService_AddDomain_Invalid(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	tests := []struct {
		name   string
		domain string
	}{
		{"empty", ""},
		{"localhost", "localhost"},
		{"ip address", "192.168.1.1"},
		{"with port", "example.com:8080"},
		{"with protocol", "https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.AddDomain(ctx, "env-123", tt.domain)
			if err == nil {
				t.Errorf("AddDomain(%q) expected error, got nil", tt.domain)
			}
			if !errors.Is(err, ErrInvalidDomain) {
				t.Errorf("expected ErrInvalidDomain, got %v", err)
			}
		})
	}
}

func TestService_AddDomain_Duplicate(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add first domain
	_, err := svc.AddDomain(ctx, "env-123", "example.com")
	if err != nil {
		t.Fatalf("first AddDomain() error = %v", err)
	}

	// Try to add duplicate
	_, err = svc.AddDomain(ctx, "env-456", "example.com")
	if err == nil {
		t.Error("expected error for duplicate domain")
	}
	if !errors.Is(err, ErrDomainAlreadyExists) {
		t.Errorf("expected ErrDomainAlreadyExists, got %v", err)
	}
}

func TestService_GetDomain(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add a domain
	created, _ := svc.AddDomain(ctx, "env-123", "example.com")

	// Get it back
	domain, err := svc.GetDomain(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDomain() error = %v", err)
	}
	if domain.Domain != "example.com" {
		t.Errorf("expected domain 'example.com', got %q", domain.Domain)
	}
}

func TestService_GetDomain_NotFound(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	_, err := svc.GetDomain(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent domain")
	}
}

func TestService_GetDomainsByEnvironment(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add domains to different environments
	svc.AddDomain(ctx, "env-123", "example1.com")
	svc.AddDomain(ctx, "env-123", "example2.com")
	svc.AddDomain(ctx, "env-456", "other.com")

	// Get domains for env-123
	domains, err := svc.GetDomainsByEnvironment(ctx, "env-123")
	if err != nil {
		t.Fatalf("GetDomainsByEnvironment() error = %v", err)
	}
	if len(domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(domains))
	}
}

func TestService_DeleteDomain(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add a domain
	created, _ := svc.AddDomain(ctx, "env-123", "example.com")

	// Delete it
	err := svc.DeleteDomain(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeleteDomain() error = %v", err)
	}

	// Verify it's gone
	_, err = svc.GetDomain(ctx, created.ID)
	if err == nil {
		t.Error("expected error getting deleted domain")
	}
}

func TestService_SetPrimaryDomain(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add domains
	d1, _ := svc.AddDomain(ctx, "env-123", "example1.com")
	d2, _ := svc.AddDomain(ctx, "env-123", "example2.com")

	// Manually verify domains for testing
	d1.Status = models.DomainStatusActive
	d1.VerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	repo.Update(ctx, d1)

	d2.Status = models.DomainStatusActive
	d2.VerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	repo.Update(ctx, d2)

	// Set d1 as primary
	err := svc.SetPrimaryDomain(ctx, d1.ID)
	if err != nil {
		t.Fatalf("SetPrimaryDomain() error = %v", err)
	}

	// Verify d1 is primary
	d1, _ = svc.GetDomain(ctx, d1.ID)
	if !d1.Primary {
		t.Error("expected d1 to be primary")
	}

	// Set d2 as primary
	err = svc.SetPrimaryDomain(ctx, d2.ID)
	if err != nil {
		t.Fatalf("SetPrimaryDomain() error = %v", err)
	}

	// Verify d2 is now primary and d1 is not
	d1, _ = svc.GetDomain(ctx, d1.ID)
	d2, _ = svc.GetDomain(ctx, d2.ID)
	if d1.Primary {
		t.Error("expected d1 to no longer be primary")
	}
	if !d2.Primary {
		t.Error("expected d2 to be primary")
	}
}

func TestService_SetPrimaryDomain_NotVerified(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add unverified domain
	d, _ := svc.AddDomain(ctx, "env-123", "example.com")

	// Try to set as primary
	err := svc.SetPrimaryDomain(ctx, d.ID)
	if err == nil {
		t.Error("expected error setting unverified domain as primary")
	}
	if !errors.Is(err, ErrDomainNotVerified) {
		t.Errorf("expected ErrDomainNotVerified, got %v", err)
	}
}

func TestService_UpdateDomainSettings(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add domain
	d, _ := svc.AddDomain(ctx, "env-123", "example.com")

	// Update settings
	err := svc.UpdateDomainSettings(ctx, d.ID, Settings{
		ForceHTTPS:  false,
		WWWRedirect: "www",
	})
	if err != nil {
		t.Fatalf("UpdateDomainSettings() error = %v", err)
	}

	// Verify changes
	d, _ = svc.GetDomain(ctx, d.ID)
	if d.ForceHTTPS {
		t.Error("expected ForceHTTPS to be false")
	}
	if !d.RedirectsWWW() {
		t.Error("expected WWW redirect to be set")
	}
}

func TestService_UpdateSSLStatus(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add domain
	d, _ := svc.AddDomain(ctx, "env-123", "example.com")

	// Update SSL status
	expiresAt := time.Now().Add(90 * 24 * time.Hour)
	err := svc.UpdateSSLStatus(ctx, d.ID, models.SSLStatusActive, &expiresAt)
	if err != nil {
		t.Fatalf("UpdateSSLStatus() error = %v", err)
	}

	// Verify changes
	d, _ = svc.GetDomain(ctx, d.ID)
	if d.SSLStatus != models.SSLStatusActive {
		t.Errorf("expected SSL status active, got %v", d.SSLStatus)
	}
	if !d.SSLProvisionedAt.Valid {
		t.Error("expected SSLProvisionedAt to be set")
	}
	if !d.SSLExpiresAt.Valid {
		t.Error("expected SSLExpiresAt to be set")
	}
}

func TestService_GetExpiringSSLDomains(t *testing.T) {
	repo := newMockRepository()
	svc := NewService(repo, DefaultConfig())
	ctx := context.Background()

	// Add domains with different SSL expiry dates
	d1, _ := svc.AddDomain(ctx, "env-123", "expiring.com")
	d1.SSLStatus = models.SSLStatusActive
	d1.SSLExpiresAt = sql.NullTime{Time: time.Now().Add(7 * 24 * time.Hour), Valid: true}
	repo.Update(ctx, d1)

	d2, _ := svc.AddDomain(ctx, "env-123", "notexpiring.com")
	d2.SSLStatus = models.SSLStatusActive
	d2.SSLExpiresAt = sql.NullTime{Time: time.Now().Add(90 * 24 * time.Hour), Valid: true}
	repo.Update(ctx, d2)

	// Get expiring domains (config defaults to 30 day warning)
	domains, err := svc.GetExpiringSSLDomains(ctx)
	if err != nil {
		t.Fatalf("GetExpiringSSLDomains() error = %v", err)
	}
	if len(domains) != 1 {
		t.Errorf("expected 1 expiring domain, got %d", len(domains))
	}
	if len(domains) > 0 && domains[0].Domain != "expiring.com" {
		t.Errorf("expected 'expiring.com', got %q", domains[0].Domain)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxVerificationAttempts != 10 {
		t.Errorf("expected MaxVerificationAttempts 10, got %d", cfg.MaxVerificationAttempts)
	}
	if cfg.VerificationTTL != 7*24*time.Hour {
		t.Errorf("expected VerificationTTL 7 days, got %v", cfg.VerificationTTL)
	}
	if cfg.CheckInterval != 5*time.Minute {
		t.Errorf("expected CheckInterval 5 minutes, got %v", cfg.CheckInterval)
	}
	if cfg.SSLExpiryWarning != 30*24*time.Hour {
		t.Errorf("expected SSLExpiryWarning 30 days, got %v", cfg.SSLExpiryWarning)
	}
}

func TestGetVerificationInstructions(t *testing.T) {
	domain := &models.CustomDomain{
		Domain:             "example.com",
		VerificationRecord: "goforge-verify=abc123",
	}

	instructions := GetVerificationInstructions(domain)

	if instructions == "" {
		t.Error("expected non-empty instructions")
	}
	if !contains(instructions, "example.com") {
		t.Error("expected instructions to contain domain")
	}
	if !contains(instructions, "_goforge-verify.example.com") {
		t.Error("expected instructions to contain verification record name")
	}
	if !contains(instructions, "goforge-verify=abc123") {
		t.Error("expected instructions to contain verification record value")
	}
}

func TestIsWildcardDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"*.example.com", true},
		{"example.com", false},
		{"app.example.com", false},
		{"*example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := IsWildcardDomain(tt.domain); got != tt.want {
				t.Errorf("IsWildcardDomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func TestGetApexDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   string
	}{
		{"example.com", "example.com"},
		{"app.example.com", "example.com"},
		{"api.v1.example.com", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := GetApexDomain(tt.domain); got != tt.want {
				t.Errorf("GetApexDomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
