package audit

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

// ChainKeyInfo is the HKDF info string for the audit hash-chain key.
const ChainKeyInfo = "goforge-audit-chain-v1"

// ChainKeyLen is the HMAC-SHA256 key length.
const ChainKeyLen = 32

// KeyEpoch is the epoch stored on seals made with ChainKeyInfo.
const KeyEpoch = 1

// DeriveChainKey derives the audit chain key from ENCRYPTION_KEY.
// The key is not stored. Rotating ENCRYPTION_KEY is not supported; seals
// keep KeyEpoch so a later rotation can be distinguished.
func DeriveChainKey(encryptionKey string) ([]byte, error) {
	if encryptionKey == "" {
		return nil, errors.New("encryption key is required to seal the audit log")
	}
	key, err := hkdf.Key(sha256.New, []byte(encryptionKey), nil, ChainKeyInfo, ChainKeyLen)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// Genesis is the prev_hash of the first seal, and the boundary before any
// retention purge. Callers must not retain and mutate the slice.
func Genesis() []byte {
	return make([]byte, ChainKeyLen)
}
