package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Password hashing parameters
const (
	timeIterations = 1
	memoryKB       = 64 * 1024
	parallelism    = 4
	saltLength     = 16
	keyLength      = 32
)

var (
	// ErrInvalidHash is returned when the encoded hash is not in the correct format.
	ErrInvalidHash = errors.New("the encoded hash is not in the correct format")
	// ErrIncompatibleVersion is returned when the argon2 version is incompatible.
	ErrIncompatibleVersion = errors.New("incompatible version of argon2")
)

// HashPassword generates a specialized Argon2id hash of a password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, timeIterations, memoryKB, parallelism, keyLength)

	// Base64 encode the salt and hash
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	// Return a string using the standard argon2 format
	encodedHash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKB, timeIterations, parallelism, b64Salt, b64Hash)

	return encodedHash, nil
}

// ComparePassword compares a password with an Argon2id hash.
func ComparePassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}

	var m, t, p uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}

	if p > 255 {
		return false, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}

	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}

	if uint64(len(decodedHash)) > 0xFFFFFFFF {
		return false, ErrInvalidHash
	}

	hash := argon2.IDKey([]byte(password), salt, t, m, uint8(p), uint32(len(decodedHash)))

	if subtle.ConstantTimeCompare(hash, decodedHash) == 1 {
		return true, nil
	}

	return false, nil
}
