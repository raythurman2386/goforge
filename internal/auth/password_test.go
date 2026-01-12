package auth_test

import (
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestArgon2Dependency(t *testing.T) {
	// Simple test to ensure the dependency is available and works
	password := []byte("password")
	salt := []byte("somesalt")

	// Use Argon2id as specified in the spec
	hash := argon2.IDKey(password, salt, 1, 64*1024, 4, 32)

	if len(hash) != 32 {
		t.Errorf("Expected hash length 32, got %d", len(hash))
	}
}
