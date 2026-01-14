// Package secrets provides encryption and masking for sensitive data.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Common errors.
var (
	ErrInvalidKey       = errors.New("encryption key must be 16, 24, or 32 bytes")
	ErrInvalidCipher    = errors.New("invalid ciphertext")
	ErrDecryptionFailed = errors.New("decryption failed")
)

// MaskChar is the character used to mask secret values.
const MaskChar = '*'

// MaskLength is the number of mask characters to show.
const MaskLength = 8

// Encryptor handles encryption and decryption of secrets.
type Encryptor struct {
	key []byte
	gcm cipher.AEAD
}

// NewEncryptor creates a new Encryptor with the given key.
// The key must be 16, 24, or 32 bytes for AES-128, AES-192, or AES-256.
func NewEncryptor(key []byte) (*Encryptor, error) {
	keyLen := len(key)
	if keyLen != 16 && keyLen != 24 && keyLen != 32 {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Encryptor{
		key: key,
		gcm: gcm,
	}, nil
}

// NewEncryptorFromString creates an Encryptor from a base64-encoded key string.
func NewEncryptorFromString(keyStr string) (*Encryptor, error) {
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	return NewEncryptor(key)
}

// GenerateKey generates a new random encryption key of the specified size.
// Size must be 16, 24, or 32 bytes.
func GenerateKey(size int) ([]byte, error) {
	if size != 16 && size != 24 && size != 32 {
		return nil, ErrInvalidKey
	}
	key := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// GenerateKeyString generates a new random encryption key and returns it as base64.
func GenerateKeyString(size int) (string, error) {
	key, err := GenerateKey(size)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// Encrypt encrypts plaintext and returns base64-encoded ciphertext.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	// Generate a random nonce
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the plaintext
	ciphertext := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Return base64-encoded ciphertext
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext and returns plaintext.
func (e *Encryptor) Decrypt(ciphertext string) (string, error) {
	// Decode base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceSize := e.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCipher
	}

	// Extract nonce and ciphertext
	nonce, encryptedData := data[:nonceSize], data[nonceSize:]

	// Decrypt
	plaintext, err := e.gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}

	return string(plaintext), nil
}

// Mask returns a masked version of the value.
// Only the first few characters are shown (if value is long enough).
func Mask(value string) string {
	if value == "" {
		return ""
	}
	return strings.Repeat(string(MaskChar), MaskLength)
}

// MaskPartial returns a partially masked version of the value.
// Shows the first `showChars` characters followed by mask characters.
func MaskPartial(value string, showChars int) string {
	if value == "" {
		return ""
	}
	if len(value) <= showChars {
		return strings.Repeat(string(MaskChar), MaskLength)
	}
	return value[:showChars] + strings.Repeat(string(MaskChar), MaskLength-showChars)
}

// MaskEnds returns a masked version showing first and last few characters.
func MaskEnds(value string, showChars int) string {
	if value == "" {
		return ""
	}
	if len(value) <= showChars*2 {
		return strings.Repeat(string(MaskChar), MaskLength)
	}
	return value[:showChars] + strings.Repeat(string(MaskChar), 4) + value[len(value)-showChars:]
}

// IsMasked checks if a value appears to be masked.
func IsMasked(value string) bool {
	if value == "" {
		return false
	}
	// Check if value is all mask characters
	for _, c := range value {
		if c != MaskChar {
			return false
		}
	}
	return true
}

// IsEncrypted checks if a value appears to be encrypted (base64 format).
// This is a heuristic check, not a guarantee.
func IsEncrypted(value string) bool {
	if value == "" {
		return false
	}
	// Check if value looks like base64
	_, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(value) > 32 // Encrypted values are typically longer
}
