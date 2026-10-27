package repositories

import (
	"encoding/json"

	"github.com/raythurman2386/goforge/internal/secrets"
)

// sealedPayload is valid JSONB that holds ciphertext instead of a plaintext object.
type sealedPayload struct {
	Enc string `json:"_goforge_enc"`
}

// sealJSON encrypts a JSON document. The in-memory value is left unchanged;
// callers pass the returned copy to SQL. A nil encryptor stores the original bytes.
func sealJSON(enc *secrets.Encryptor, raw json.RawMessage) (json.RawMessage, error) {
	if enc == nil || len(raw) == 0 || isSealed(raw) {
		return raw, nil
	}
	ciphertext, err := enc.Encrypt(string(raw))
	if err != nil {
		return nil, err
	}
	return json.Marshal(sealedPayload{Enc: ciphertext})
}

// openJSON decrypts a sealed document. Legacy plaintext objects are returned as-is.
func openJSON(enc *secrets.Encryptor, raw json.RawMessage) json.RawMessage {
	if enc == nil || !isSealed(raw) {
		return raw
	}
	var payload sealedPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	plain, err := enc.Decrypt(payload.Enc)
	if err != nil {
		return raw
	}
	return json.RawMessage(plain)
}

func isSealed(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 1 {
		return false
	}
	_, ok := fields["_goforge_enc"]
	return ok
}
