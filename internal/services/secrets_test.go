package services

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// verifyPHC checks plain against an Argon2id PHC string the way Vaultwarden does.
func verifyPHC(t *testing.T, phc, plain string) bool {
	t.Helper()
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		t.Errorf("not an argon2id PHC string: %q", phc)
		return false
	}
	var v int
	var m, tc uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		t.Errorf("version segment %q", parts[2])
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &tc, &p); err != nil {
		t.Errorf("params segment %q: %v", parts[3], err)
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Errorf("salt: %v", err)
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		t.Errorf("hash: %v", err)
		return false
	}
	got := argon2.IDKey([]byte(plain), salt, tc, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func TestArgon2idPHC(t *testing.T) {
	t.Parallel()
	a, err := argon2idPHC("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	b, err := argon2idPHC("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("PHC = %q, want OWASP-preset argon2id", a)
	}
	if a == b {
		t.Error("two hashes share a salt")
	}
	if !verifyPHC(t, a, "s3cret") || verifyPHC(t, a, "wrong") {
		t.Error("PHC does not verify only the original value")
	}
	if empty, _ := argon2idPHC(""); empty != "" {
		t.Errorf("argon2id(\"\") = %q, want empty so an unset token stays unset", empty)
	}
}

func TestIsSecretField(t *testing.T) {
	t.Parallel()
	tmpl := &ServiceTemplate{Config: []ConfigField{
		{Name: "admin_token", Type: "password"},
		{Name: "pin", Type: "password"},
		{Name: "username", Type: "text"},
	}}
	tests := []struct {
		tmpl *ServiceTemplate
		name string
		want bool
	}{
		{tmpl, "admin_token", true},
		{tmpl, "pin", true},
		{tmpl, "username", false},
		{tmpl, "domain", false},
		{nil, "root_password", true},
		{nil, "jwt_secret", true},
		{nil, "api_token", true},
		{nil, "database", false},
	}
	for _, tt := range tests {
		if got := IsSecretField(tt.tmpl, tt.name); got != tt.want {
			t.Errorf("IsSecretField(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRedactSecrets(t *testing.T) {
	t.Parallel()
	tmpl := &ServiceTemplate{Config: []ConfigField{{Name: "password", Type: "password"}}}
	config := map[string]interface{}{"username": "app", "password": "hunter2", "database": "app"}
	secrets := SecretValues(tmpl, config)
	got := RedactSecrets("postgres://app:hunter2@db:5432/app", secrets)
	if want := "postgres://app:" + MaskedValue + "@db:5432/app"; got != want {
		t.Errorf("RedactSecrets = %q, want %q", got, want)
	}
	if got := RedactSecrets("http://vw:80", secrets); got != "http://vw:80" {
		t.Errorf("RedactSecrets changed a string with no secret: %q", got)
	}
	if len(SecretValues(tmpl, map[string]interface{}{"password": ""})) != 0 {
		t.Error("empty password counted as a secret to redact")
	}
}
