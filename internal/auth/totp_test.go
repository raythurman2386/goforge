package auth

import (
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"
)

// rfcSecret is the RFC 6238 appendix B SHA-1 test key "12345678901234567890".
var rfcSecret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

func TestTOTPCodeRFC6238Vectors(t *testing.T) {
	// RFC 6238 appendix B gives 8 digits; GoForge uses the last 6.
	vectors := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, v := range vectors {
		got, err := TOTPCode(rfcSecret, TOTPStep(time.Unix(v.unix, 0)))
		if err != nil {
			t.Fatal(err)
		}
		if got != v.want {
			t.Errorf("TOTPCode(t=%d) = %s, want %s", v.unix, got, v.want)
		}
	}
}

func TestVerifyTOTPClockSkew(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	step := TOTPStep(now)
	code := func(s int64) string {
		c, err := TOTPCode(rfcSecret, s)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	for _, off := range []int64{-1, 0, 1} {
		got, ok := VerifyTOTP(rfcSecret, code(step+off), now, 0)
		if !ok || got != step+off {
			t.Errorf("offset %d: VerifyTOTP = (%d, %v), want (%d, true)", off, got, ok, step+off)
		}
	}
	for _, off := range []int64{-3, -2, 2, 3} {
		if _, ok := VerifyTOTP(rfcSecret, code(step+off), now, 0); ok {
			t.Errorf("offset %d: VerifyTOTP accepted a code outside ±1 step", off)
		}
	}
	// 29 s of drift either way still lands within one step.
	if _, ok := VerifyTOTP(rfcSecret, code(step), now.Add(29*time.Second), 0); !ok {
		t.Error("code rejected 29 s late")
	}
	if _, ok := VerifyTOTP(rfcSecret, code(step), now.Add(-29*time.Second), 0); !ok {
		t.Error("code rejected 29 s early")
	}
}

func TestVerifyTOTPRejectsReplayAndJunk(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	step := TOTPStep(now)
	c, _ := TOTPCode(rfcSecret, step)
	if _, ok := VerifyTOTP(rfcSecret, c, now, step); ok {
		t.Error("VerifyTOTP accepted a code for an already used step")
	}
	if _, ok := VerifyTOTP(rfcSecret, c, now, step-1); !ok {
		t.Error("VerifyTOTP rejected a fresh code")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef", "12 456"} {
		if _, ok := VerifyTOTP(rfcSecret, bad, now, 0); ok {
			t.Errorf("VerifyTOTP accepted %q", bad)
		}
	}
	if _, ok := VerifyTOTP("not base32!", "123456", now, 0); ok {
		t.Error("VerifyTOTP accepted a bad secret")
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	s, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 32 || strings.Contains(s, "=") {
		t.Fatalf("secret %q: want 32 unpadded base32 chars", s)
	}
	s2, _ := GenerateTOTPSecret()
	if s == s2 {
		t.Fatal("two secrets matched")
	}
}

func TestOTPAuthURI(t *testing.T) {
	u, err := url.Parse(OTPAuthURI("a+b@example.com", rfcSecret))
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("uri = %s", u)
	}
	if u.Path != "/GoForge:a+b@example.com" {
		t.Errorf("label = %q", u.Path)
	}
	q := u.Query()
	if q.Get("secret") != rfcSecret || q.Get("issuer") != "GoForge" || q.Get("digits") != "6" || q.Get("period") != "30" {
		t.Errorf("query = %v", q)
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d codes", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 11 || c[5] != '-' {
			t.Errorf("code %q not shaped xxxxx-xxxxx", c)
		}
		if IsTOTPCodeShape(c) {
			t.Errorf("code %q looks like a TOTP code", c)
		}
		seen[c] = true
	}
	if len(seen) != len(codes) {
		t.Error("duplicate recovery codes")
	}
	h := HashRecoveryCode("abcde-fghij")
	for _, in := range []string{"ABCDE-FGHIJ", "abcde fghij", " abcdefghij "} {
		if HashRecoveryCode(in) != h {
			t.Errorf("HashRecoveryCode(%q) did not normalize", in)
		}
	}
	if len(h) != 64 {
		t.Errorf("hash length %d", len(h))
	}
}

func TestIsTOTPCodeShape(t *testing.T) {
	for in, want := range map[string]bool{"123456": true, " 123 456 ": true, "12345": false, "abcde-fghij": false, "1234567": false} {
		if got := IsTOTPCodeShape(in); got != want {
			t.Errorf("IsTOTPCodeShape(%q) = %v", in, got)
		}
	}
}
