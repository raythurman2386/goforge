// Package config provides configuration loading from environment variables.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
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

	// GitHub OAuth
	GitHubClientID     string
	GitHubClientSecret string

	// Docker
	DockerHost string

	// Traefik
	TraefikNetwork string
	AcmeEmail      string

	// Service Templates
	TemplatesDir string

	// Deployment Limits
	MaxConcurrentBuilds int
	BuildTimeout        time.Duration

	// Monitoring
	EnableMetrics    bool
	MetricsRetention time.Duration

	// Logging
	LogLevel  string
	LogFormat string

	// Development mode - relaxes security requirements
	DevMode bool
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	devMode := getEnvBool("GOCOOL_DEV_MODE", false)

	cfg := &Config{
		// Server defaults
		Host:    getEnv("GOCOOL_HOST", "0.0.0.0"),
		Port:    getEnvInt("GOCOOL_PORT", 8080),
		BaseURL: getEnv("GOCOOL_BASE_URL", "http://localhost:8080"),

		// Database - defaults to SQLite in dev mode
		DatabaseURL: getEnv("DATABASE_URL", ""),

		// Security
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		CSRFSecret:    os.Getenv("CSRF_SECRET"),

		// GitHub OAuth
		GitHubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),

		// Docker
		DockerHost: getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),

		// Traefik
		TraefikNetwork: getEnv("TRAEFIK_NETWORK", "traefik"),
		AcmeEmail:      os.Getenv("ACME_EMAIL"),

		// Service Templates
		TemplatesDir: getEnv("TEMPLATES_DIR", "./templates"),

		// Deployment Limits
		MaxConcurrentBuilds: getEnvInt("MAX_CONCURRENT_BUILDS", 2),
		BuildTimeout:        getEnvDuration("BUILD_TIMEOUT", 15*time.Minute),

		// Monitoring
		EnableMetrics:    getEnvBool("ENABLE_METRICS", true),
		MetricsRetention: getEnvDuration("METRICS_RETENTION", 168*time.Hour), // 7 days

		// Logging
		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogFormat: getEnv("LOG_FORMAT", "json"),

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
		c.DatabaseURL = "postgres://gocool:password@localhost:5432/gocool?sslmode=disable"
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
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.EncryptionKey == "" {
		return fmt.Errorf("ENCRYPTION_KEY is required")
	}
	if c.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}
	if c.CSRFSecret == "" {
		return fmt.Errorf("CSRF_SECRET is required")
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
