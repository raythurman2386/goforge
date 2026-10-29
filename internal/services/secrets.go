package services

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/raythurman2386/goforge/internal/models"
)

// FieldTypePassword is the config field type for secrets.
const FieldTypePassword = "password"

// MaskedValue replaces a secret value wherever the service page would show it.
const MaskedValue = "••••••••"

// secretNameHints mark a config key as secret when no template field says so,
// for example after the template was removed from the catalog.
var secretNameHints = []string{FieldTypePassword, "passwd", "secret", "token"}

// IsSecretField reports whether config key name holds a secret. Template fields
// of type password are secret; other keys fall back to a name check.
func IsSecretField(tmpl *ServiceTemplate, name string) bool {
	if tmpl != nil {
		for _, field := range tmpl.Config {
			if field.Name == name && field.Type == FieldTypePassword {
				return true
			}
		}
	}
	lower := strings.ToLower(name)
	for _, hint := range secretNameHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

// SecretValues returns the non-empty secret values in config, longest first so
// a redaction pass never leaves part of a longer secret behind.
func SecretValues(tmpl *ServiceTemplate, config map[string]interface{}) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(config))
	for k, v := range config {
		if !IsSecretField(tmpl, k) {
			continue
		}
		s := fmt.Sprintf("%v", v)
		if v == nil || s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// RedactSecrets replaces every occurrence of each secret in s with MaskedValue.
func RedactSecrets(s string, secrets []string) string {
	for _, secret := range secrets {
		s = strings.ReplaceAll(s, secret, MaskedValue)
	}
	return s
}

// Sources of a value shown under Connection Details.
const (
	SecretSourceConfig     = "config"
	SecretSourceConnection = "connection"
)

// DetailRow is one Connection Details entry. Display is safe to print: a
// secret row shows MaskedValue (or a connection string with its secrets
// masked) and the real value is fetched only on reveal or copy.
type DetailRow struct {
	Source  string
	Key     string
	Display string
	Secret  bool
}

// DetailRows returns the config and connection rows for the service page,
// sorted by key, with every secret masked.
func DetailRows(tmpl *ServiceTemplate, inst *models.ServiceInstance) (config, connection []DetailRow) {
	cfg, err := inst.GetConfig()
	if err != nil {
		cfg = nil
	}
	for _, k := range sortedKeys(cfg) {
		v := fmt.Sprintf("%v", cfg[k])
		row := DetailRow{Source: SecretSourceConfig, Key: k, Display: v}
		if IsSecretField(tmpl, k) && cfg[k] != nil && v != "" {
			row.Display, row.Secret = MaskedValue, true
		}
		config = append(config, row)
	}
	connection = maskedConnectionRows(tmpl, inst, cfg)
	return config, connection
}

// MaskedConnectionInfo returns the instance's connection strings with every
// secret masked, for pages that list them without a reveal control.
func MaskedConnectionInfo(tmpl *ServiceTemplate, inst *models.ServiceInstance) map[string]string {
	cfg, err := inst.GetConfig()
	if err != nil {
		cfg = nil
	}
	out := make(map[string]string)
	for _, row := range maskedConnectionRows(tmpl, inst, cfg) {
		out[row.Key] = row.Display
	}
	return out
}

func maskedConnectionRows(tmpl *ServiceTemplate, inst *models.ServiceInstance, cfg map[string]interface{}) []DetailRow {
	info, err := inst.GetConnectionInfo()
	if err != nil {
		return nil
	}
	secrets := SecretValues(tmpl, cfg)
	keys := make([]string, 0, len(info))
	for k := range info {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]DetailRow, 0, len(keys))
	for _, k := range keys {
		masked := RedactSecrets(info[k], secrets)
		rows = append(rows, DetailRow{Source: SecretSourceConnection, Key: k, Display: masked, Secret: masked != info[k]})
	}
	return rows
}

// SecretValue returns the plain value behind a masked row. Only secret config
// fields and connection strings that contain a secret are returned, so the
// reveal endpoint cannot be used to read anything the page does not mask.
func SecretValue(tmpl *ServiceTemplate, inst *models.ServiceInstance, source, key string) (string, bool) {
	config, connection := DetailRows(tmpl, inst)
	rows := config
	if source == SecretSourceConnection {
		rows = connection
	} else if source != SecretSourceConfig {
		return "", false
	}
	found := false
	for _, row := range rows {
		if row.Key == key && row.Secret {
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	if source == SecretSourceConnection {
		info, err := inst.GetConnectionInfo()
		if err != nil {
			return "", false
		}
		return info[key], true
	}
	cfg, err := inst.GetConfig()
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("%v", cfg[key]), true
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Argon2id PHC parameters for derived secrets, matching the OWASP preset of
// `vaultwarden hash --preset owasp` (19 MiB, 2 passes, 1 lane).
const (
	phcMemoryKB = 19 * 1024
	phcTime     = 2
	phcThreads  = 1
	phcSaltLen  = 16
	phcKeyLen   = 32
)

// argon2idPHC hashes value into an Argon2id PHC string with a random salt, so
// a template can hand a service the hash while GoForge keeps the plain token.
// An empty value stays empty so "unset" keeps its meaning for the service.
func argon2idPHC(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	salt := make([]byte, phcSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2id salt: %w", err)
	}
	key := argon2.IDKey([]byte(value), salt, phcTime, phcMemoryKB, phcThreads, phcKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, phcMemoryKB, phcTime, phcThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}
