package secrets

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/raythurman2386/goforge/internal/models"
)

// Common secret key patterns that should be auto-detected.
var secretKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(api[_-]?key|apikey)$`),
	regexp.MustCompile(`(?i)^(secret[_-]?key|secretkey)$`),
	regexp.MustCompile(`(?i)^(password|passwd|pwd)$`),
	regexp.MustCompile(`(?i)^(token|auth[_-]?token|access[_-]?token)$`),
	regexp.MustCompile(`(?i)^(private[_-]?key|ssh[_-]?key)$`),
	regexp.MustCompile(`(?i)^(database[_-]?url|db[_-]?url|connection[_-]?string)$`),
	regexp.MustCompile(`(?i)^(aws[_-]?secret|aws[_-]?access[_-]?key)$`),
	regexp.MustCompile(`(?i)^(stripe[_-]?key|stripe[_-]?secret)$`),
	regexp.MustCompile(`(?i)^(sendgrid[_-]?api[_-]?key)$`),
	regexp.MustCompile(`(?i)^(twilio[_-]?(auth[_-]?token|api[_-]?key))$`),
	regexp.MustCompile(`(?i)^(github[_-]?token|gitlab[_-]?token)$`),
	regexp.MustCompile(`(?i)^(encryption[_-]?key|signing[_-]?key)$`),
	regexp.MustCompile(`(?i)^(client[_-]?secret|oauth[_-]?secret)$`),
	regexp.MustCompile(`(?i).*_(secret|key|token|password|pwd)$`),
}

// Service provides secret management functionality.
type Service struct {
	encryptor *Encryptor
}

// NewService creates a new secrets Service.
func NewService(encryptor *Encryptor) *Service {
	return &Service{
		encryptor: encryptor,
	}
}

// NewServiceFromKey creates a new secrets Service from a key string.
func NewServiceFromKey(keyStr string) (*Service, error) {
	enc, err := NewEncryptorFromString(keyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}
	return NewService(enc), nil
}

// EncryptVariable encrypts a secret environment variable value.
func (s *Service) EncryptVariable(_ context.Context, v *models.EnvVariable) error {
	if !v.IsSecret {
		return nil
	}

	// Don't re-encrypt already encrypted values
	if IsEncrypted(v.Value) {
		return nil
	}

	encrypted, err := s.encryptor.Encrypt(v.Value)
	if err != nil {
		return fmt.Errorf("failed to encrypt variable %s: %w", v.Key, err)
	}
	v.Value = encrypted
	return nil
}

// DecryptVariable decrypts a secret environment variable value.
func (s *Service) DecryptVariable(_ context.Context, v *models.EnvVariable) error {
	if !v.IsSecret {
		return nil
	}

	// Don't try to decrypt non-encrypted values
	if !IsEncrypted(v.Value) {
		return nil
	}

	decrypted, err := s.encryptor.Decrypt(v.Value)
	if err != nil {
		return fmt.Errorf("failed to decrypt variable %s: %w", v.Key, err)
	}
	v.Value = decrypted
	return nil
}

// MaskVariable returns a masked version of the variable for display.
func (s *Service) MaskVariable(_ context.Context, v *models.EnvVariable) *models.EnvVariable {
	masked := *v
	if v.IsSecret {
		masked.Value = Mask(v.Value)
	}
	return &masked
}

// MaskVariables returns masked versions of multiple variables.
func (s *Service) MaskVariables(_ context.Context, vars []*models.EnvVariable) []*models.EnvVariable {
	masked := make([]*models.EnvVariable, len(vars))
	for i, v := range vars {
		m := *v
		if v.IsSecret {
			m.Value = Mask(v.Value)
		}
		masked[i] = &m
	}
	return masked
}

// PrepareForStorage encrypts all secret variables before storage.
func (s *Service) PrepareForStorage(ctx context.Context, vars []*models.EnvVariable) error {
	for _, v := range vars {
		if err := s.EncryptVariable(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

// PrepareForUse decrypts all secret variables for use (e.g., deployment).
func (s *Service) PrepareForUse(ctx context.Context, vars []*models.EnvVariable) error {
	for _, v := range vars {
		if err := s.DecryptVariable(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

// ShouldBeSecret determines if a variable key should be marked as secret.
func ShouldBeSecret(key string) bool {
	for _, pattern := range secretKeyPatterns {
		if pattern.MatchString(key) {
			return true
		}
	}
	return false
}

// DetectSecrets marks variables that should be secrets based on key names.
func DetectSecrets(vars []*models.EnvVariable) {
	for _, v := range vars {
		if !v.IsSecret && ShouldBeSecret(v.Key) {
			v.IsSecret = true
		}
	}
}

// ToEnvFormat converts variables to KEY=VALUE format for container deployment.
// Secret values are decrypted.
func (s *Service) ToEnvFormat(ctx context.Context, vars []*models.EnvVariable) ([]string, error) {
	result := make([]string, 0, len(vars))
	for _, v := range vars {
		value := v.Value
		if v.IsSecret && IsEncrypted(value) {
			decrypted, err := s.encryptor.Decrypt(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt %s: %w", v.Key, err)
			}
			value = decrypted
		}
		result = append(result, fmt.Sprintf("%s=%s", v.Key, value))
	}
	return result, nil
}

// ToEnvMap converts variables to a map for container deployment.
// Secret values are decrypted.
func (s *Service) ToEnvMap(ctx context.Context, vars []*models.EnvVariable) (map[string]string, error) {
	result := make(map[string]string, len(vars))
	for _, v := range vars {
		value := v.Value
		if v.IsSecret && IsEncrypted(value) {
			decrypted, err := s.encryptor.Decrypt(value)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt %s: %w", v.Key, err)
			}
			value = decrypted
		}
		result[v.Key] = value
	}
	return result, nil
}

// SanitizeLogOutput replaces secret values in log output with masked versions.
func (s *Service) SanitizeLogOutput(_ context.Context, output string, vars []*models.EnvVariable) string {
	sanitized := output
	for _, v := range vars {
		if !v.IsSecret {
			continue
		}

		// Get the decrypted value to match against
		value := v.Value
		if IsEncrypted(value) {
			decrypted, err := s.encryptor.Decrypt(value)
			if err == nil {
				value = decrypted
			}
		}

		if value != "" {
			sanitized = strings.ReplaceAll(sanitized, value, Mask(value))
		}
	}
	return sanitized
}

// ValidateSecretValue validates that a secret value meets requirements.
func ValidateSecretValue(value string) error {
	if value == "" {
		return fmt.Errorf("secret value cannot be empty")
	}
	if IsMasked(value) {
		return fmt.Errorf("secret value cannot be a masked value")
	}
	return nil
}
