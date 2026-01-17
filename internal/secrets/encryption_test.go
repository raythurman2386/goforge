package secrets

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raythurman2386/goforge/internal/config"
)

func TestEncryptionService(t *testing.T) {
	key := make([]byte, 32)
	_, err := base64.StdEncoding.DecodeString("YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY=")
	require.NoError(t, err)

	cfg := &config.Config{
		EncryptionKey: base64.StdEncoding.EncodeToString(key),
	}

	service, err := NewEncryptionService(cfg)
	require.NoError(t, err)

	t.Run("Encrypt and Decrypt successfully", func(t *testing.T) {
		plaintext := "my-super-secret-token"
		encrypted, err := service.Encrypt(plaintext)
		require.NoError(t, err)

		decrypted, err := service.Decrypt(encrypted)
		require.NoError(t, err)

		assert.Equal(t, plaintext, decrypted)
		assert.NotEqual(t, plaintext, encrypted)
	})

	t.Run("Fails with wrong key", func(t *testing.T) {
		wrongKey := make([]byte, 32)
		_, err := base64.StdEncoding.DecodeString("NjU0MzIxegB4d3Z1dHNycXBvbm1sa2ppaGdmZWRjYmE=")
		require.NoError(t, err)
		wrongCfg := &config.Config{
			EncryptionKey: base64.StdEncoding.EncodeToString(wrongKey),
		}
		wrongService, err := NewEncryptionService(wrongCfg)
		require.NoError(t, err)

		plaintext := "my-super-secret-token"
		encrypted, err := service.Encrypt(plaintext)
		require.NoError(t, err)

		_, err = wrongService.Decrypt(encrypted)
		assert.Error(t, err)
	})
}
