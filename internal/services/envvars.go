package services

import (
	"strings"
	"unicode"

	"github.com/raythurman2386/goforge/internal/models"
)

// ServiceEnvVarName returns the environment variable name to use when injecting a
// service connection string into a project's container environment.
//
// The name is always prefixed with a sanitized form of the instance name to avoid
// collisions when multiple instances of the same service type are linked to one project
// (e.g. two Postgres instances become MYDB_DATABASE_URL and REPLICA_DATABASE_URL).
//
// For the "internal" connection key, a conventional suffix is chosen:
//   - postgres / postgresql / mysql / mariadb / mongodb → DATABASE_URL
//   - redis  → REDIS_URL
//   - minio  → MINIO_URL
//   - nocodb → NOCODB_URL
//   - other  → <TEMPLATE>_URL
//
// For any other connection key (e.g. "external") the suffix is the uppercased key.
func ServiceEnvVarName(instanceName, templateName, connKey string) string {
	prefix := sanitizeEnvName(instanceName)
	suffix := conventionalSuffix(templateName, connKey)
	return prefix + "_" + suffix
}

const (
	templateNameRedis    = "redis"
	templateNamePostgres = "postgres"
)

// conventionalSuffix returns the conventional env var suffix for a service's connection key.
func conventionalSuffix(templateName, connKey string) string {
	if connKey != "internal" {
		return strings.ToUpper(strings.ReplaceAll(connKey, ".", "_"))
	}
	switch strings.ToLower(templateName) {
	case templateNamePostgres, "postgresql", "mysql", "mariadb", "mongodb":
		return "DATABASE_URL"
	case templateNameRedis:
		return "REDIS_URL"
	case "minio":
		return "MINIO_URL"
	case "nocodb":
		return "NOCODB_URL"
	default:
		return sanitizeEnvName(templateName) + "_URL"
	}
}

// sanitizeEnvName converts an arbitrary string into a valid env var name segment:
// uppercased, non-alphanumeric runs replaced with underscores, leading/trailing
// underscores trimmed.
func sanitizeEnvName(s string) string {
	s = strings.ToUpper(s)
	var b strings.Builder
	prevUnderscore := true // suppress leading underscores
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevUnderscore = false
		} else if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	result := b.String()
	return strings.TrimRight(result, "_")
}

// LinkEnvVars returns the env vars that linking inst to a project injects,
// keyed by variable name. Values are connection strings and are secret.
func LinkEnvVars(inst *models.ServiceInstance) (map[string]string, error) {
	connInfo, err := inst.GetConnectionInfo()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(connInfo))
	for connKey, connVal := range connInfo {
		out[ServiceEnvVarName(inst.Name, inst.TemplateName, connKey)] = connVal
	}
	return out, nil
}
