package git

import (
	"strings"
	"testing"
)

func TestGenerateSSHKeyPair(t *testing.T) {
	t.Parallel()

	keyPair, err := GenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair() error = %v", err)
	}

	// Private key should be non-empty and in PEM format
	if keyPair.PrivateKey == "" {
		t.Error("PrivateKey is empty")
	}
	if !strings.Contains(keyPair.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("PrivateKey not in OpenSSH format: %q", keyPair.PrivateKey[:50])
	}

	// Public key should be non-empty and in SSH format
	if keyPair.PublicKey == "" {
		t.Error("PublicKey is empty")
	}
	if !strings.HasPrefix(keyPair.PublicKey, "ssh-ed25519 ") {
		t.Errorf("PublicKey not in ssh-ed25519 format: %q", keyPair.PublicKey[:30])
	}

	// Fingerprint should be non-empty
	if keyPair.Fingerprint == "" {
		t.Error("Fingerprint is empty")
	}
	if !strings.HasPrefix(keyPair.Fingerprint, "SHA256:") {
		t.Errorf("Fingerprint not in SHA256 format: %q", keyPair.Fingerprint)
	}
}

func TestParseSSHPrivateKey_Valid(t *testing.T) {
	t.Parallel()

	// Generate a key to parse
	keyPair, err := GenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair() error = %v", err)
	}

	parsed, err := ParseSSHPrivateKey(keyPair.PrivateKey)
	if err != nil {
		t.Fatalf("ParseSSHPrivateKey() error = %v", err)
	}

	if parsed == nil {
		t.Fatal("ParseSSHPrivateKey() returned nil signer")
	}
}

func TestParseSSHPrivateKey_InvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "not PEM format",
			input:   "just some random text",
			wantErr: true,
		},
		{
			name:    "garbage PEM header",
			input:   "-----BEGIN GARBAGE-----\nAAA=\n-----END GARBAGE-----",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseSSHPrivateKey(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSSHPrivateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetFingerprint(t *testing.T) {
	t.Parallel()

	keyPair, err := GenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair() error = %v", err)
	}

	fp, err := GetFingerprint(keyPair.PublicKey)
	if err != nil {
		t.Fatalf("GetFingerprint() error = %v", err)
	}

	if !strings.HasPrefix(fp, "SHA256:") {
		t.Errorf("GetFingerprint() = %q, want SHA256: prefix", fp)
	}

	// Should match the generated fingerprint
	if fp != keyPair.Fingerprint {
		t.Errorf("GetFingerprint() = %q, want %q", fp, keyPair.Fingerprint)
	}
}

func TestGetFingerprint_InvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "not-a-valid-key",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := GetFingerprint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetFingerprint() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSSHKeyPair_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyPair SSHKeyPair
		wantErr bool
	}{
		{
			name: "empty private key",
			keyPair: SSHKeyPair{
				PrivateKey:  "",
				PublicKey:   "ssh-ed25519 AAAA...",
				Fingerprint: "SHA256:...",
			},
			wantErr: true,
		},
		{
			name: "empty public key",
			keyPair: SSHKeyPair{
				PrivateKey:  "-----BEGIN OPENSSH PRIVATE KEY-----...",
				PublicKey:   "",
				Fingerprint: "SHA256:...",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.keyPair.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewSSHAuth(t *testing.T) {
	t.Parallel()

	keyPair, err := GenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPair() error = %v", err)
	}

	auth, err := NewSSHAuth(keyPair.PrivateKey)
	if err != nil {
		t.Fatalf("NewSSHAuth() error = %v", err)
	}

	if auth == nil {
		t.Fatal("NewSSHAuth() returned nil")
	}

	// The auth should have the expected name
	if auth.Name() != "ssh-public-keys" {
		t.Errorf("auth.Name() = %q, want ssh-public-keys", auth.Name())
	}
}
