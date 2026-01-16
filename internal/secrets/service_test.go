package secrets

import (
	"context"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

const (
	testMaskedValue = "********"
	testSecretValue = "sk-12345"
)

func TestNewService(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)

	if svc == nil {
		t.Error("NewService() returned nil")
	}
}

func TestNewServiceFromKey(t *testing.T) {
	keyStr, _ := GenerateKeyString(32)
	svc, err := NewServiceFromKey(keyStr)
	if err != nil {
		t.Errorf("NewServiceFromKey() error: %v", err)
	}
	if svc == nil {
		t.Error("NewServiceFromKey() returned nil")
	}
}

func TestNewServiceFromKeyInvalid(t *testing.T) {
	_, err := NewServiceFromKey("invalid-key")
	if err == nil {
		t.Error("NewServiceFromKey() with invalid key expected error")
	}
}

func TestEncryptDecryptVariable(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		variable *models.EnvVariable
		wantEnc  bool
	}{
		{
			name: "encrypt secret variable",
			variable: &models.EnvVariable{
				Key:      "API_KEY",
				Value:    "sk-12345",
				IsSecret: true,
			},
			wantEnc: true,
		},
		{
			name: "skip non-secret variable",
			variable: &models.EnvVariable{
				Key:      "APP_ENV",
				Value:    "production",
				IsSecret: false,
			},
			wantEnc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalValue := tt.variable.Value

			// Encrypt
			err := svc.EncryptVariable(ctx, tt.variable)
			if err != nil {
				t.Errorf("EncryptVariable() error: %v", err)
				return
			}

			if tt.wantEnc {
				if tt.variable.Value == originalValue {
					t.Error("EncryptVariable() did not encrypt the value")
				}
				if !IsEncrypted(tt.variable.Value) {
					t.Error("EncryptVariable() value does not appear encrypted")
				}
			} else {
				if tt.variable.Value != originalValue {
					t.Error("EncryptVariable() modified non-secret value")
				}
			}

			// Decrypt
			err = svc.DecryptVariable(ctx, tt.variable)
			if err != nil {
				t.Errorf("DecryptVariable() error: %v", err)
				return
			}

			if tt.variable.Value != originalValue {
				t.Errorf("DecryptVariable() = %q, want %q", tt.variable.Value, originalValue)
			}
		})
	}
}

func TestEncryptVariableIdempotent(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	v := &models.EnvVariable{
		Key:      "API_KEY",
		Value:    "sk-12345",
		IsSecret: true,
	}

	// Encrypt once
	err := svc.EncryptVariable(ctx, v)
	if err != nil {
		t.Fatalf("EncryptVariable() error: %v", err)
	}
	firstEncryption := v.Value

	// Encrypt again (should not double-encrypt)
	err = svc.EncryptVariable(ctx, v)
	if err != nil {
		t.Fatalf("EncryptVariable() second call error: %v", err)
	}

	if v.Value != firstEncryption {
		t.Error("EncryptVariable() modified already-encrypted value")
	}
}

func TestMaskVariable(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	tests := []struct {
		name     string
		variable *models.EnvVariable
		wantMask bool
	}{
		{
			name: "mask secret",
			variable: &models.EnvVariable{
				Key:      "PASSWORD",
				Value:    "super-secret",
				IsSecret: true,
			},
			wantMask: true,
		},
		{
			name: "don't mask non-secret",
			variable: &models.EnvVariable{
				Key:      "APP_NAME",
				Value:    "myapp",
				IsSecret: false,
			},
			wantMask: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masked := svc.MaskVariable(ctx, tt.variable)

			// Original should not be modified
			if tt.variable.Value == testMaskedValue {
				t.Error("MaskVariable() modified original variable")
			}

			if tt.wantMask {
				if masked.Value != testMaskedValue {
					t.Errorf("MaskVariable() = %q, want %q", masked.Value, testMaskedValue)
				}
			} else {
				if masked.Value != tt.variable.Value {
					t.Errorf("MaskVariable() modified non-secret value")
				}
			}
		})
	}
}

func TestMaskVariables(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: "sk-12345", IsSecret: true},
		{Key: "APP_ENV", Value: "production", IsSecret: false},
		{Key: "DB_PASSWORD", Value: "secret123", IsSecret: true},
	}

	masked := svc.MaskVariables(ctx, vars)

	if len(masked) != len(vars) {
		t.Errorf("MaskVariables() returned %d vars, want %d", len(masked), len(vars))
	}

	if masked[0].Value != testMaskedValue {
		t.Errorf("MaskVariables() first var = %q, want %q", masked[0].Value, testMaskedValue)
	}
	if masked[1].Value != "production" {
		t.Errorf("MaskVariables() second var = %q, want %q", masked[1].Value, "production")
	}
	if masked[2].Value != testMaskedValue {
		t.Errorf("MaskVariables() third var = %q, want %q", masked[2].Value, testMaskedValue)
	}

	// Originals should not be modified
	if vars[0].Value != testSecretValue {
		t.Error("MaskVariables() modified original variables")
	}
}

func TestPrepareForStorageAndUse(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: "sk-12345", IsSecret: true},
		{Key: "APP_ENV", Value: "production", IsSecret: false},
		{Key: "DB_PASSWORD", Value: "secret123", IsSecret: true},
	}

	originalValues := []string{vars[0].Value, vars[1].Value, vars[2].Value}

	// Prepare for storage (encrypt)
	err := svc.PrepareForStorage(ctx, vars)
	if err != nil {
		t.Fatalf("PrepareForStorage() error: %v", err)
	}

	// Secrets should be encrypted
	if vars[0].Value == originalValues[0] {
		t.Error("PrepareForStorage() did not encrypt first secret")
	}
	if vars[1].Value != originalValues[1] {
		t.Error("PrepareForStorage() modified non-secret")
	}
	if vars[2].Value == originalValues[2] {
		t.Error("PrepareForStorage() did not encrypt second secret")
	}

	// Prepare for use (decrypt)
	err = svc.PrepareForUse(ctx, vars)
	if err != nil {
		t.Fatalf("PrepareForUse() error: %v", err)
	}

	// Should be back to original
	if vars[0].Value != originalValues[0] {
		t.Errorf("PrepareForUse() first var = %q, want %q", vars[0].Value, originalValues[0])
	}
	if vars[1].Value != originalValues[1] {
		t.Errorf("PrepareForUse() second var = %q, want %q", vars[1].Value, originalValues[1])
	}
	if vars[2].Value != originalValues[2] {
		t.Errorf("PrepareForUse() third var = %q, want %q", vars[2].Value, originalValues[2])
	}
}

func TestShouldBeSecret(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"API_KEY", true},
		{"api_key", true},
		{"apikey", true},
		{"PASSWORD", true},
		{"password", true},
		{"TOKEN", true},
		{"auth_token", true},
		{"access_token", true},
		{"SECRET_KEY", true},
		{"PRIVATE_KEY", true},
		{"DATABASE_URL", true},
		{"AWS_SECRET", true},
		{"STRIPE_KEY", true},
		{"GITHUB_TOKEN", true},
		{"MY_API_SECRET", true},
		{"DB_PASSWORD", true},
		{"CUSTOM_TOKEN", true},
		// Non-secrets
		{"APP_NAME", false},
		{"APP_ENV", false},
		{"PORT", false},
		{"HOST", false},
		{"DEBUG", false},
		{"LOG_LEVEL", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := ShouldBeSecret(tt.key)
			if got != tt.want {
				t.Errorf("ShouldBeSecret(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestDetectSecrets(t *testing.T) {
	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: "sk-12345", IsSecret: false},
		{Key: "APP_ENV", Value: "production", IsSecret: false},
		{Key: "PASSWORD", Value: "secret", IsSecret: false},
		{Key: "PORT", Value: "8080", IsSecret: false},
	}

	DetectSecrets(vars)

	if !vars[0].IsSecret {
		t.Error("DetectSecrets() did not detect API_KEY as secret")
	}
	if vars[1].IsSecret {
		t.Error("DetectSecrets() incorrectly marked APP_ENV as secret")
	}
	if !vars[2].IsSecret {
		t.Error("DetectSecrets() did not detect PASSWORD as secret")
	}
	if vars[3].IsSecret {
		t.Error("DetectSecrets() incorrectly marked PORT as secret")
	}
}

func TestToEnvFormat(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	// Create vars with one encrypted secret
	secretValue := testSecretValue
	encryptedValue, _ := enc.Encrypt(secretValue)

	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: encryptedValue, IsSecret: true},
		{Key: "APP_ENV", Value: "production", IsSecret: false},
	}

	envList, err := svc.ToEnvFormat(ctx, vars)
	if err != nil {
		t.Fatalf("ToEnvFormat() error: %v", err)
	}

	if len(envList) != 2 {
		t.Fatalf("ToEnvFormat() returned %d items, want 2", len(envList))
	}

	// Secret should be decrypted
	if envList[0] != "API_KEY=sk-12345" {
		t.Errorf("ToEnvFormat() first item = %q, want %q", envList[0], "API_KEY=sk-12345")
	}
	if envList[1] != "APP_ENV=production" {
		t.Errorf("ToEnvFormat() second item = %q, want %q", envList[1], "APP_ENV=production")
	}
}

func TestToEnvMap(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	// Create vars with one encrypted secret
	secretValue := testSecretValue
	encryptedValue, _ := enc.Encrypt(secretValue)

	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: encryptedValue, IsSecret: true},
		{Key: "APP_ENV", Value: "production", IsSecret: false},
	}

	envMap, err := svc.ToEnvMap(ctx, vars)
	if err != nil {
		t.Fatalf("ToEnvMap() error: %v", err)
	}

	if len(envMap) != 2 {
		t.Fatalf("ToEnvMap() returned %d items, want 2", len(envMap))
	}

	if envMap["API_KEY"] != testSecretValue {
		t.Errorf("ToEnvMap()[API_KEY] = %q, want %q", envMap["API_KEY"], testSecretValue)
	}
	if envMap["APP_ENV"] != "production" {
		t.Errorf("ToEnvMap()[APP_ENV] = %q, want %q", envMap["APP_ENV"], "production")
	}
}

func TestSanitizeLogOutput(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	secretValue := testSecretValue
	encryptedValue, _ := enc.Encrypt(secretValue)

	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: encryptedValue, IsSecret: true},
		{Key: "APP_ENV", Value: "production", IsSecret: false},
	}

	logOutput := "Connecting with API key sk-12345 to server production"

	sanitized := svc.SanitizeLogOutput(ctx, logOutput, vars)

	if sanitized == logOutput {
		t.Error("SanitizeLogOutput() did not modify output")
	}
	if !contains(sanitized, "********") {
		t.Error("SanitizeLogOutput() did not mask secret")
	}
	if !contains(sanitized, "production") {
		t.Error("SanitizeLogOutput() removed non-secret value")
	}
}

func TestValidateSecretValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid value", "my-secret", false},
		{"empty value", "", true},
		{"masked value", "********", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSecretValue(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSecretValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func BenchmarkPrepareForStorage(b *testing.B) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vars := []*models.EnvVariable{
			{Key: "API_KEY", Value: "sk-12345", IsSecret: true},
			{Key: "APP_ENV", Value: "production", IsSecret: false},
			{Key: "DB_PASSWORD", Value: "secret123", IsSecret: true},
		}
		_ = svc.PrepareForStorage(ctx, vars)
	}
}

func BenchmarkSanitizeLogOutput(b *testing.B) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	svc := NewService(enc)
	ctx := context.Background()

	secretValue := testSecretValue
	encryptedValue, _ := enc.Encrypt(secretValue)

	vars := []*models.EnvVariable{
		{Key: "API_KEY", Value: encryptedValue, IsSecret: true},
	}

	logOutput := "Connecting with API key sk-12345 to server"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = svc.SanitizeLogOutput(ctx, logOutput, vars)
	}
}
