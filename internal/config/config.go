// Package config provides configuration loading from environment variables.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// DatabaseType represents the database backend.
type DatabaseType string

const (
	// DatabaseTypePostgres uses PostgreSQL database.
	DatabaseTypePostgres DatabaseType = "postgres"
)

// Config holds all application configuration.
type Config struct {
	// Server settings
	Host    string
	Port    int
	BaseURL string

	// Database
	DatabaseURL  string
	DatabaseType DatabaseType

	// Security
	EncryptionKey string
	SessionSecret string
	CSRFSecret    string
	WebhookSecret string

	// GitHub OAuth
	GitHubClientID     string
	GitHubClientSecret string

	// Docker
	DockerHost string

	// Traefik
	TraefikNetwork string
	AcmeEmail      string

	// Cloudflare Tunnel
	CloudflareProxyMode bool

	// Service Templates
	TemplatesDir string

	// Build Directory
	BuildDir string

	// Deployment Limits
	MaxConcurrentBuilds int
	BuildTimeout        time.Duration

	// Monitoring
	EnableMetrics    bool
	MetricsRetention time.Duration

	// Logging
	LogLevel  string
	LogFormat string

	// Trusted Proxies - CIDR ranges whose X-Forwarded-For/X-Real-IP headers are trusted.
	// If empty, private/loopback networks are trusted by default (suitable for local Traefik).
	// Set to explicit CIDRs in production if your proxy is on a non-private network.
	TrustedProxies []string

	// Development mode - relaxes security requirements
	DevMode bool
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	devMode := getEnvBool("GOFORGE_DEV_MODE", false)

	cfg := &Config{
		// Server defaults
		Host:    getEnv("GOFORGE_HOST", "0.0.0.0"),
		Port:    getEnvInt("GOFORGE_PORT", 8080),
		BaseURL: getEnv("GOFORGE_BASE_URL", "http://localhost:8080"),

		// Database (PostgreSQL)
		DatabaseURL: getEnv("DATABASE_URL", ""),

		// Security
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		CSRFSecret:    os.Getenv("CSRF_SECRET"),
		WebhookSecret: os.Getenv("WEBHOOK_SECRET"),

		// GitHub OAuth
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),

		// Docker
		DockerHost: getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),

		// Traefik
		TraefikNetwork: getEnv("TRAEFIK_NETWORK", "traefik"),
		AcmeEmail:      os.Getenv("ACME_EMAIL"),

		// Cloudflare Tunnel
		CloudflareProxyMode: getEnvBool("CLOUDFLARE_PROXY_MODE", false),

		// Service Templates
		TemplatesDir: getEnv("TEMPLATES_DIR", "./templates"),

		// Build Directory
		BuildDir: getEnv("GOFORGE_BUILD_DIR", os.TempDir()),

		// Deployment Limits
		MaxConcurrentBuilds: getEnvInt("MAX_CONCURRENT_BUILDS", 2),
		BuildTimeout:        getEnvDuration("BUILD_TIMEOUT", 15*time.Minute),

		// Monitoring
		EnableMetrics:    getEnvBool("ENABLE_METRICS", true),
		MetricsRetention: getEnvDuration("METRICS_RETENTION", 168*time.Hour), // 7 days

		// Logging
		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogFormat: getEnv("LOG_FORMAT", "json"),

		// Trusted proxies (comma-separated CIDRs or IPs)
		TrustedProxies: getEnvStringSlice("TRUSTED_PROXIES"),

		// Dev mode
		DevMode: devMode,
	}

	// Apply defaults and dev mode settings
	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// applyDefaults sets default values, especially for dev mode.
func (c *Config) applyDefaults() {
	// Determine database type and set defaults
	if c.DatabaseURL == "" {
		// Default to Postgres in dev
		c.DatabaseURL = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	c.DatabaseType = DatabaseTypePostgres

	// In dev mode, generate random secrets if not provided
	if c.DevMode {
		if c.EncryptionKey == "" {
			c.EncryptionKey = generateRandomKey(32)
		}
		if c.SessionSecret == "" {
			c.SessionSecret = generateRandomKey(32)
		}
		if c.CSRFSecret == "" {
			c.CSRFSecret = generateRandomKey(32)
		}
		if c.WebhookSecret == "" {
			c.WebhookSecret = generateRandomKey(32)
		}
		// Use text logging in dev mode for readability
		if os.Getenv("LOG_FORMAT") == "" {
			c.LogFormat = "text"
		}
		if os.Getenv("LOG_LEVEL") == "" {
			c.LogLevel = "debug"
		}
	}
}

// validate checks that required configuration is present.
func (c *Config) validate() error {
	// In dev mode, we're more lenient
	if c.DevMode {
		return nil
	}

	// Production validation
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.EncryptionKey == "" {
		return errors.New("ENCRYPTION_KEY is required")
	}
	if c.SessionSecret == "" {
		return errors.New("SESSION_SECRET is required")
	}
	if c.CSRFSecret == "" {
		return errors.New("CSRF_SECRET is required")
	}
	return nil
}

// Addr returns the server address in host:port format.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// IsDevelopment returns true if running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.DevMode || c.LogLevel == "debug"
}

// getEnv returns the environment variable value or a default.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt returns the environment variable as an int or a default.
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// getEnvBool returns the environment variable as a bool or a default.
func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

// getEnvDuration returns the environment variable as a duration or a default.
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

// getEnvStringSlice returns the environment variable split by commas, with whitespace trimmed.
// Returns nil if the variable is empty or unset.
func getEnvStringSlice(key string) []string {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// generateRandomKey generates a random base64-encoded key.
//
//nolint:unparam // length is currently always 32 but this is intended to be a flexible utility
func generateRandomKey(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a deterministic but unique-ish key for dev
		return fmt.Sprintf("dev-key-%d", time.Now().UnixNano())
	}
	return base64.StdEncoding.EncodeToString(bytes)
}
