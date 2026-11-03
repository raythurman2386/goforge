package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1; authenticator apps expect it.
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238 defaults, which every authenticator app reads).
const (
	TOTPPeriod     = 30 * time.Second
	TOTPDigits     = 6
	TOTPSkewSteps  = 1 // accept one step either side of now (±30 s)
	totpSecretSize = 20
	totpIssuer     = "GoForge"
)

// RecoveryCodeCount is how many one-time recovery codes enrollment creates.
const RecoveryCodeCount = 10

var b32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a new random 160-bit secret as unpadded base32,
// the form authenticator apps take for manual entry.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, totpSecretSize)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32NoPad.EncodeToString(b), nil
}

// TOTPStep returns the RFC 6238 time step for t.
func TOTPStep(t time.Time) int64 {
	return t.Unix() / int64(TOTPPeriod/time.Second)
}

// TOTPCode returns the 6-digit code for secret at time step.
func TOTPCode(secret string, step int64) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, step), nil
}

// VerifyTOTP checks code against secret at now, allowing TOTPSkewSteps of
// clock skew either way. It returns the matched step, which must be newer
// than lastUsedStep so the same code (or an older one) cannot be replayed.
func VerifyTOTP(secret, code string, now time.Time, lastUsedStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != TOTPDigits || !allDigits(code) {
		return 0, false
	}
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return 0, false
	}
	cur := TOTPStep(now)
	var matched int64
	ok := false
	for off := int64(-TOTPSkewSteps); off <= TOTPSkewSteps; off++ {
		step := cur + off
		if step <= lastUsedStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(hotp(key, step)), []byte(code)) == 1 && !ok {
			matched, ok = step, true
		}
	}
	return matched, ok
}

// OTPAuthURI is the otpauth:// URI an authenticator app scans from the QR
// code. The label is "GoForge:<email>".
func OTPAuthURI(email, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", totpIssuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(TOTPDigits))
	v.Set("period", fmt.Sprint(int(TOTPPeriod/time.Second)))
	label := url.PathEscape(totpIssuer + ":" + email)
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// FormatTOTPSecret groups a base32 secret in fours for manual entry.
func FormatTOTPSecret(secret string) string {
	var b strings.Builder
	for i, r := range secret {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// GenerateRecoveryCodes returns n random codes shaped "xxxxx-xxxxx"
// (lowercase base32, 50 bits each).
func GenerateRecoveryCodes(n int) ([]string, error) {
	codes := make([]string, 0, n)
	for len(codes) < n {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s := strings.ToLower(b32NoPad.EncodeToString(b))[:10]
		codes = append(codes, s[:5]+"-"+s[5:])
	}
	return codes, nil
}

// NormalizeRecoveryCode lowercases a typed recovery code and drops spaces
// and dashes, so "ABCDE-FGHIJ", "abcde fghij", and "abcdefghij" match.
func NormalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(code) {
		if r == '-' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// HashRecoveryCode is the stored form of a recovery code: SHA-256 hex of
// the normalized code.
func HashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(NormalizeRecoveryCode(code)))
	return fmt.Sprintf("%x", sum)
}

// IsTOTPCodeShape reports whether input looks like a 6-digit TOTP code
// (spaces allowed) rather than a recovery code.
func IsTOTPCodeShape(input string) bool {
	s := strings.ReplaceAll(strings.TrimSpace(input), " ", "")
	return len(s) == TOTPDigits && allDigits(s)
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	s = strings.TrimRight(s, "=")
	key, err := b32NoPad.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid TOTP secret: %w", err)
	}
	return key, nil
}

func hotp(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", bin%1000000)
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
