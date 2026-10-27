package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_DevMode(t *testing.T) {
	os.Clearenv()
	os.Setenv("GOFORGE_DEV_MODE", "true")
	os.Setenv("GOFORGE_DEV_SECRETS_FILE", filepath.Join(t.TempDir(), "dev-secrets.json"))
	defer os.Clearenv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error in dev mode: %v", err)
	}

	if !cfg.DevMode {
		t.Error("expected DevMode to be true")
	}

	// Should have auto-generated secrets
	if cfg.EncryptionKey == "" {
		t.Error("expected EncryptionKey to be auto-generated")
	}
	if cfg.SessionSecret == "" {
		t.Error("expected SessionSecret to be auto-generated")
	}
	if cfg.CSRFSecret == "" {
		t.Error("expected CSRFSecret to be auto-generated")
	}

	// Should use text logging in dev mode
	if cfg.LogFormat != "text" {
		t.Errorf("expected text log format in dev mode, got %s", cfg.LogFormat)
	}
}

func TestLoad_RequiredFields_Production(t *testing.T) {
	os.Clearenv()
	// Not in dev mode - should require all fields

	_, err := Load()
	if err == nil {
		t.Error("expected error when required fields are missing in production")
	}
}

func TestLoad_AllRequiredFields(t *testing.T) {
	os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
	os.Setenv("ENCRYPTION_KEY", "test-encryption-key-32-bytes-xx")
	os.Setenv("SESSION_SECRET", "test-session-secret")
	os.Setenv("CSRF_SECRET", "test-csrf-secret")
	defer os.Clearenv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://test:test@localhost:5432/test" {
		t.Errorf("unexpected DatabaseURL: %s", cfg.DatabaseURL)
	}

	if cfg.DatabaseType != DatabaseTypePostgres {
		t.Errorf("expected postgres database type, got %s", cfg.DatabaseType)
	}
}

func TestLoad_Defaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("GOFORGE_DEV_MODE", "true")
	os.Setenv("GOFORGE_DEV_SECRETS_FILE", filepath.Join(t.TempDir(), "dev-secrets.json"))
	defer os.Clearenv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check defaults
	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected default host 0.0.0.0, got %s", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.MaxConcurrentBuilds != 2 {
		t.Errorf("expected default max concurrent builds 2, got %d", cfg.MaxConcurrentBuilds)
	}
	if cfg.BuildTimeout != 15*time.Minute {
		t.Errorf("expected default build timeout 15m, got %v", cfg.BuildTimeout)
	}
	if cfg.EnableRegistration {
		t.Error("expected registration to be closed by default")
	}
	if cfg.AdminEmail != "" {
		t.Errorf("expected empty admin email, got %q", cfg.AdminEmail)
	}
}

func TestLoad_AdminEmail(t *testing.T) {
	os.Clearenv()
	t.Cleanup(os.Clearenv)
	os.Setenv("GOFORGE_DEV_MODE", "true")
	os.Setenv("GOFORGE_DEV_SECRETS_FILE", filepath.Join(t.TempDir(), "dev-secrets.json"))
	os.Setenv("GOFORGE_ADMIN_EMAIL", " Owner@Example.com ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AdminEmail != "Owner@Example.com" {
		t.Fatalf("AdminEmail = %q", cfg.AdminEmail)
	}
}

func TestLoad_DevSecretsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dev-secrets.json")

	os.Clearenv()
	t.Cleanup(os.Clearenv)
	os.Setenv("GOFORGE_DEV_MODE", "true")
	os.Setenv("GOFORGE_DEV_SECRETS_FILE", path)

	first, err := Load()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	os.Clearenv()
	os.Setenv("GOFORGE_DEV_MODE", "true")
	os.Setenv("GOFORGE_DEV_SECRETS_FILE", path)

	second, err := Load()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	if first.EncryptionKey == "" || first.EncryptionKey != second.EncryptionKey {
		t.Fatal("encryption key was not reused from the dev secrets file")
	}
	if first.SessionSecret != second.SessionSecret || first.CSRFSecret != second.CSRFSecret || first.WebhookSecret != second.WebhookSecret {
		t.Fatal("dev secrets were not reused")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat secrets file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file mode = %o, want 600", info.Mode().Perm())
	}
}

func TestConfig_Addr(t *testing.T) {
	cfg := &Config{
		Host: "127.0.0.1",
		Port: 3000,
	}

	if cfg.Addr() != "127.0.0.1:3000" {
		t.Errorf("unexpected addr: %s", cfg.Addr())
	}
}

func TestConfig_IsDevelopment(t *testing.T) {
	tests := []struct {
		name     string
		devMode  bool
		logLevel string
		want     bool
	}{
		{"dev mode enabled", true, "info", true},
		{"debug log level", false, "debug", true},
		{"production", false, "info", false},
		{"production warn", false, "warn", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{DevMode: tt.devMode, LogLevel: tt.logLevel}
			if got := cfg.IsDevelopment(); got != tt.want {
				t.Errorf("IsDevelopment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetEnvHelpers(t *testing.T) {
	os.Clearenv()

	// Test getEnv
	os.Setenv("TEST_VAR", "test-value")
	if v := getEnv("TEST_VAR", "default"); v != "test-value" {
		t.Errorf("getEnv returned %s, want test-value", v)
	}
	if v := getEnv("MISSING_VAR", "default"); v != "default" {
		t.Errorf("getEnv returned %s, want default", v)
	}

	// Test getEnvInt
	os.Setenv("TEST_INT", "42")
	if v := getEnvInt("TEST_INT", 10); v != 42 {
		t.Errorf("getEnvInt returned %d, want 42", v)
	}
	if v := getEnvInt("MISSING_INT", 10); v != 10 {
		t.Errorf("getEnvInt returned %d, want 10", v)
	}

	// Test getEnvBool
	os.Setenv("TEST_BOOL", "true")
	if v := getEnvBool("TEST_BOOL", false); v != true {
		t.Errorf("getEnvBool returned %v, want true", v)
	}
	if v := getEnvBool("MISSING_BOOL", true); v != true {
		t.Errorf("getEnvBool returned %v, want true", v)
	}

	// Test getEnvDuration
	os.Setenv("TEST_DUR", "5m")
	if v := getEnvDuration("TEST_DUR", time.Hour); v != 5*time.Minute {
		t.Errorf("getEnvDuration returned %v, want 5m", v)
	}
	if v := getEnvDuration("MISSING_DUR", time.Hour); v != time.Hour {
		t.Errorf("getEnvDuration returned %v, want 1h", v)
	}

	os.Clearenv()
}

func TestGenerateRandomKey(t *testing.T) {
	key1 := generateRandomKey(32)
	key2 := generateRandomKey(32)

	if key1 == "" {
		t.Error("generateRandomKey returned empty string")
	}

	if key1 == key2 {
		t.Error("generateRandomKey should produce unique keys")
	}

	// Check that it's base64 encoded (should be longer than input length)
	if len(key1) < 32 {
		t.Errorf("expected key length >= 32, got %d", len(key1))
	}
}
