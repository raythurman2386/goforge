package secrets

import (
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name    string
		keySize int
		wantErr bool
	}{
		{"valid 16-byte key (AES-128)", 16, false},
		{"valid 24-byte key (AES-192)", 24, false},
		{"valid 32-byte key (AES-256)", 32, false},
		{"invalid 8-byte key", 8, true},
		{"invalid 15-byte key", 15, true},
		{"invalid 20-byte key", 20, true},
		{"invalid 64-byte key", 64, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := make([]byte, tt.keySize)
			for i := range key {
				key[i] = byte(i)
			}

			enc, err := NewEncryptor(key)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewEncryptor() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewEncryptor() unexpected error: %v", err)
				return
			}
			if enc == nil {
				t.Errorf("NewEncryptor() returned nil encryptor")
			}
		})
	}
}

func TestNewEncryptorFromString(t *testing.T) {
	tests := []struct {
		name    string
		keyStr  string
		wantErr bool
	}{
		{"valid base64 32-byte key", "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=", false},
		{"valid base64 16-byte key", "MDEyMzQ1Njc4OTAxMjM0NQ==", false},
		{"invalid base64", "not-valid-base64!!!", true},
		{"invalid key size after decode", "YWJjZA==", true}, // "abcd" = 4 bytes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewEncryptorFromString(tt.keyStr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewEncryptorFromString() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewEncryptorFromString() unexpected error: %v", err)
				return
			}
			if enc == nil {
				t.Errorf("NewEncryptorFromString() returned nil encryptor")
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{"generate 16-byte key", 16, false},
		{"generate 24-byte key", 24, false},
		{"generate 32-byte key", 32, false},
		{"invalid 8-byte key", 8, true},
		{"invalid 20-byte key", 20, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := GenerateKey(tt.size)
			if tt.wantErr {
				if err == nil {
					t.Errorf("GenerateKey() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("GenerateKey() unexpected error: %v", err)
				return
			}
			if len(key) != tt.size {
				t.Errorf("GenerateKey() key length = %d, want %d", len(key), tt.size)
			}
		})
	}
}

func TestGenerateKeyString(t *testing.T) {
	keyStr, err := GenerateKeyString(32)
	if err != nil {
		t.Fatalf("GenerateKeyString() unexpected error: %v", err)
	}
	if keyStr == "" {
		t.Error("GenerateKeyString() returned empty string")
	}

	// Should be able to create encryptor from generated key
	enc, err := NewEncryptorFromString(keyStr)
	if err != nil {
		t.Errorf("NewEncryptorFromString() failed with generated key: %v", err)
	}
	if enc == nil {
		t.Error("NewEncryptorFromString() returned nil for generated key")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateKey(32)
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	enc, err := NewEncryptor(key)
	if err != nil {
		t.Fatalf("NewEncryptor() failed: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple string", "hello world"},
		{"password", "super-secret-password-123!@#"},
		{"api key", "sk-1234567890abcdef1234567890abcdef"},
		{"json", `{"key": "value", "number": 123}`},
		{"unicode", "hello 世界 🌍"},
		{"long string", "a" + string(make([]byte, 1000))},
		{"special characters", "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := enc.Encrypt(tt.plaintext)
			if err != nil {
				t.Errorf("Encrypt() error: %v", err)
				return
			}

			// Ciphertext should be different from plaintext
			if ciphertext == tt.plaintext && tt.plaintext != "" {
				t.Error("Encrypt() ciphertext equals plaintext")
			}

			decrypted, err := enc.Decrypt(ciphertext)
			if err != nil {
				t.Errorf("Decrypt() error: %v", err)
				return
			}

			if decrypted != tt.plaintext {
				t.Errorf("Decrypt() = %q, want %q", decrypted, tt.plaintext)
			}
		})
	}
}

func TestEncryptDifferentCiphertexts(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)

	plaintext := "same plaintext"

	// Encrypt the same plaintext multiple times
	ciphertexts := make([]string, 5)
	for i := range ciphertexts {
		ct, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Encrypt() error: %v", err)
		}
		ciphertexts[i] = ct
	}

	// All ciphertexts should be different (due to random nonce)
	for i := 0; i < len(ciphertexts); i++ {
		for j := i + 1; j < len(ciphertexts); j++ {
			if ciphertexts[i] == ciphertexts[j] {
				t.Errorf("Encrypt() produced same ciphertext for indices %d and %d", i, j)
			}
		}
	}

	// But all should decrypt to the same plaintext
	for i, ct := range ciphertexts {
		decrypted, err := enc.Decrypt(ct)
		if err != nil {
			t.Errorf("Decrypt() error for ciphertext %d: %v", i, err)
		}
		if decrypted != plaintext {
			t.Errorf("Decrypt() = %q, want %q for ciphertext %d", decrypted, plaintext, i)
		}
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)

	tests := []struct {
		name       string
		ciphertext string
	}{
		{"empty string", ""},
		{"invalid base64", "not-valid-base64!!!"},
		{"too short", "YWJj"},                     // "abc" base64
		{"random base64", "dGVzdGluZzEyMzQ1Ng=="}, // "testing123456" - not valid ciphertext
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enc.Decrypt(tt.ciphertext)
			if err == nil {
				t.Error("Decrypt() expected error, got nil")
			}
		})
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := GenerateKey(32)
	key2, _ := GenerateKey(32)

	enc1, _ := NewEncryptor(key1)
	enc2, _ := NewEncryptor(key2)

	plaintext := "secret message"
	ciphertext, _ := enc1.Encrypt(plaintext)

	// Should fail to decrypt with different key
	_, err := enc2.Decrypt(ciphertext)
	if err == nil {
		t.Error("Decrypt() with wrong key expected error, got nil")
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"empty string", "", ""},
		{"short string", "abc", "********"},
		{"medium string", "my-secret-key", "********"},
		{"long string", "this-is-a-very-long-secret-api-key", "********"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Mask(tt.value)
			if got != tt.want {
				t.Errorf("Mask() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaskPartial(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		showChars int
		want      string
	}{
		{"empty string", "", 3, ""},
		{"short string", "ab", 3, "********"},
		{"show first 3", "my-secret-key", 3, "my-*****"},
		{"show first 4", "my-secret-key", 4, "my-s****"},
		{"show first 0", "my-secret-key", 0, "********"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskPartial(tt.value, tt.showChars)
			if got != tt.want {
				t.Errorf("MaskPartial() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaskEnds(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		showChars int
		want      string
	}{
		{"empty string", "", 2, ""},
		{"short string", "abc", 2, "********"},
		{"show ends", "my-secret-key", 2, "my****ey"},
		{"show more ends", "abcdefghij", 3, "abc****hij"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskEnds(tt.value, tt.showChars)
			if got != tt.want {
				t.Errorf("MaskEnds() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsMasked(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty string", "", false},
		{"fully masked", "********", true},
		{"partially masked", "abc*****", false},
		{"no mask", "plaintext", false},
		{"single mask char", "*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsMasked(tt.value)
			if got != tt.want {
				t.Errorf("IsMasked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsEncrypted(t *testing.T) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	encrypted, _ := enc.Encrypt("test secret")

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty string", "", false},
		{"plain text", "hello world", false},
		{"short base64", "YWJj", false}, // Too short
		{"encrypted value", encrypted, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEncrypted(tt.value)
			if got != tt.want {
				t.Errorf("IsEncrypted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkEncrypt(b *testing.B) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	plaintext := "my-super-secret-api-key-12345678"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enc.Encrypt(plaintext)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key, _ := GenerateKey(32)
	enc, _ := NewEncryptor(key)
	ciphertext, _ := enc.Encrypt("my-super-secret-api-key-12345678")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = enc.Decrypt(ciphertext)
	}
}
