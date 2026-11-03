package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// rowHash is HMAC-SHA256(key, prev_hash || canonical). prev_hash is the raw
// 32-byte digest, not its hex form. hash.Hash.Write does not return an
// error for HMAC; it is checked because the linter requires it.
func rowHash(key, prev, canonical []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, key)
	if _, err := mac.Write(prev); err != nil {
		return nil, err
	}
	if _, err := mac.Write(canonical); err != nil {
		return nil, err
	}
	return mac.Sum(nil), nil
}

// FormatVerified is the success line printed by `goforge audit verify` and
// logged by the daily check.
func FormatVerified(seq int64, hash []byte) string {
	return fmt.Sprintf("audit chain verified head_seq=%d head_hash=%s", seq, hex.EncodeToString(hash))
}

// FormatFailed is the failure line printed by `goforge audit verify`.
func FormatFailed(seq int64, reason string) string {
	return fmt.Sprintf("audit chain failed seq=%d reason=%s", seq, reason)
}

// ChainError is a broken audit chain. Seq is the first bad seal, or 0
// when the problem is an unsealed row.
type ChainError struct {
	Seq    int64
	Reason string
}

func (f *ChainError) Error() string {
	if f == nil {
		return ""
	}
	return FormatFailed(f.Seq, f.Reason)
}
