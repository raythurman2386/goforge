package auth

import (
	"testing"
)

func TestHashPassword(t *testing.T) {
	password := "secure-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	if hash == "" {
		t.Fatal("HashPassword() returned empty string")
	}

	match, err := ComparePassword(password, hash)
	if err != nil {
		t.Fatalf("ComparePassword() error = %v", err)
	}
	if !match {
		t.Error("ComparePassword() did not match original password")
	}

	match, _ = ComparePassword("wrong-password", hash)
	if match {
		t.Error("ComparePassword() matched wrong password")
	}
}
