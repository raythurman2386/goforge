package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

// ErrInvalidSSHKey is returned when SSH key parsing fails.
var ErrInvalidSSHKey = errors.New("invalid SSH key")

// SSHKeyPair represents an SSH key pair with metadata.
type SSHKeyPair struct {
	PrivateKey  string // OpenSSH format PEM
	PublicKey   string // ssh-ed25519 format
	Fingerprint string // SHA256:...
}

// Validate checks if the key pair has required fields.
func (k *SSHKeyPair) Validate() error {
	if k.PrivateKey == "" {
		return fmt.Errorf("%w: private key is empty", ErrInvalidSSHKey)
	}
	if k.PublicKey == "" {
		return fmt.Errorf("%w: public key is empty", ErrInvalidSSHKey)
	}
	return nil
}

// GenerateSSHKeyPair generates a new Ed25519 SSH key pair.
func GenerateSSHKeyPair() (*SSHKeyPair, error) {
	// Generate Ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Convert private key to OpenSSH format
	privateKeyPEM, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Convert public key to SSH format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH public key: %w", err)
	}
	publicKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))

	// Calculate fingerprint
	fingerprint := ssh.FingerprintSHA256(sshPubKey)

	return &SSHKeyPair{
		PrivateKey:  string(pem.EncodeToMemory(privateKeyPEM)),
		PublicKey:   publicKeyStr,
		Fingerprint: fingerprint,
	}, nil
}

// ParseSSHPrivateKey parses an OpenSSH private key and returns a Signer.
func ParseSSHPrivateKey(privateKeyPEM string) (ssh.Signer, error) {
	if privateKeyPEM == "" {
		return nil, fmt.Errorf("%w: private key is empty", ErrInvalidSSHKey)
	}

	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSSHKey, err)
	}

	return signer, nil
}

// GetFingerprint extracts the SHA256 fingerprint from a public key.
func GetFingerprint(publicKey string) (string, error) {
	if publicKey == "" {
		return "", fmt.Errorf("%w: public key is empty", ErrInvalidSSHKey)
	}

	// Parse the authorized key format
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", fmt.Errorf("%w: failed to parse public key: %w", ErrInvalidSSHKey, err)
	}

	return ssh.FingerprintSHA256(pubKey), nil
}

// NewSSHAuth creates a go-git transport.AuthMethod from a private key.
func NewSSHAuth(privateKeyPEM string) (transport.AuthMethod, error) {
	signer, err := ParseSSHPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}

	// Create PublicKeys auth method using go-git's ssh package
	auth := &gitssh.PublicKeys{
		User:   "git",
		Signer: signer,
	}

	// Accept any host key (for simplicity; in production, use known_hosts)
	//nolint:gosec // G106: InsecureIgnoreHostKey used for simplicity; production should use known_hosts
	auth.HostKeyCallback = ssh.InsecureIgnoreHostKey()

	return auth, nil
}

// DecryptSSHKey decrypts an encrypted SSH private key if encrypted.
// For now, this is a passthrough as we assume keys are stored encrypted in DB
// and decrypted before use via the secrets package.
func DecryptSSHKey(encryptedKey string, decryptFunc func(string) (string, error)) (string, error) {
	if decryptFunc == nil {
		return encryptedKey, nil
	}
	return decryptFunc(encryptedKey)
}

// EncodePublicKeyForDisplay formats a public key for display in UI.
func EncodePublicKeyForDisplay(publicKey string) string {
	// Truncate long keys for display
	if len(publicKey) > 60 {
		keyType := strings.Split(publicKey, " ")[0]
		keyData := base64.StdEncoding.EncodeToString([]byte(publicKey))
		if len(keyData) > 20 {
			keyData = keyData[:10] + "..." + keyData[len(keyData)-10:]
		}
		return fmt.Sprintf("%s %s", keyType, keyData)
	}
	return publicKey
}
