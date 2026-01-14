// Package database provides database connection and management.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver

	"github.com/raythurman2386/gocool/internal/config"
)

// DB wraps the sql.DB with additional functionality.
type DB struct {
	*sql.DB
	cfg *config.Config
}

// New creates a new database connection based on configuration.
func New(cfg *config.Config) (*DB, error) {
	if cfg.DatabaseType != config.DatabaseTypePostgres {
		return nil, fmt.Errorf("only postgres is supported, got: %s", cfg.DatabaseType)
	}

	db, err := connectPostgres(cfg)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{DB: db, cfg: cfg}, nil
}

// connectPostgres establishes a PostgreSQL connection with retry logic.
func connectPostgres(cfg *config.Config) (*sql.DB, error) {
	dsn := cfg.DatabaseURL

	// Ensure sslmode is set for local development
	if !strings.Contains(dsn, "sslmode=") {
		if strings.Contains(dsn, "?") {
			dsn += "&sslmode=disable"
		} else {
			dsn += "?sslmode=disable"
		}
	}

	var db *sql.DB
	var err error

	// Retry connection with exponential backoff
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = db.PingContext(ctx)
		cancel()

		if err == nil {
			return db, nil
		}

		db.Close()
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	return nil, fmt.Errorf("failed to connect to postgres after %d attempts: %w", maxRetries, err)
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.DB.Close()
}

// IsPostgres returns true if using PostgreSQL.
func (db *DB) IsPostgres() bool {
	return db.cfg.DatabaseType == config.DatabaseTypePostgres
}

// HealthCheck verifies database connectivity.
func (db *DB) HealthCheck(ctx context.Context) error {
	return db.PingContext(ctx)
}
