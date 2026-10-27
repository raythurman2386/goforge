package repositories

import (
	"encoding/json"
	"testing"

	"github.com/raythurman2386/goforge/internal/secrets"
)

func TestSealJSONRoundTrip(t *testing.T) {
	t.Parallel()

	key, err := secrets.GenerateKey(32)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := secrets.NewEncryptor(key)
	if err != nil {
		t.Fatal(err)
	}

	plain := json.RawMessage(`{"password":"secret"}`)
	sealed, err := sealJSON(enc, plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == string(plain) {
		t.Fatal("expected ciphertext, got plaintext")
	}
	if !isSealed(sealed) {
		t.Fatalf("sealed payload not recognized: %s", sealed)
	}

	opened := openJSON(enc, sealed)
	if string(opened) != string(plain) {
		t.Fatalf("opened = %s, want %s", opened, plain)
	}

	legacy := json.RawMessage(`{"password":"legacy"}`)
	if string(openJSON(enc, legacy)) != string(legacy) {
		t.Fatal("legacy plaintext was modified")
	}
	if string(plain) != `{"password":"secret"}` {
		t.Fatal("sealJSON mutated the original document")
	}
}
