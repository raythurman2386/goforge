package services

import (
	"crypto/rand"
	"fmt"
)

const (
	passwordAlphabet    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	minGeneratedLength  = 16
	defaultPasswordSize = 24
)

// fillGeneratedConfig sets empty generate:true fields to a server-side password
// before the config is stored or interpolated into the container environment.
func fillGeneratedConfig(tmpl *ServiceTemplate, config map[string]interface{}) error {
	if tmpl == nil || config == nil {
		return nil
	}
	for _, field := range tmpl.Config {
		if !field.Generate {
			continue
		}
		if existing, ok := config[field.Name].(string); ok && existing != "" {
			continue
		}
		length := field.Length
		if length < minGeneratedLength {
			length = defaultPasswordSize
		}
		password, err := generatePassword(length)
		if err != nil {
			return fmt.Errorf("generate %s: %w", field.Name, err)
		}
		config[field.Name] = password
	}
	return nil
}

func generatePassword(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out), nil
}
