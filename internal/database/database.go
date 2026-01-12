// Package database provides database connection and management.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/lib/pq"           // PostgreSQL driver
	_ "github.com/mattn/go-sqlite3" // SQLite driver

	"github.com/user/gocool/internal/config"
)

// DB wraps the sql.DB with additional functionality.
type DB struct {
	*sql.DB
	cfg *config.Config
}

// New creates a new database connection based on configuration.
func New(cfg *config.Config) (*DB, error) {
	var db *sql.DB
	var err error

	switch cfg.DatabaseType {
	case config.DatabaseTypeSQLite:
		db, err = connectSQLite(cfg)
	case config.DatabaseTypePostgres:
		db, err = connectPostgres(cfg)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.DatabaseType)
	}

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

// connectSQLite establishes a SQLite connection.
func connectSQLite(cfg *config.Config) (*sql.DB, error) {
	dbPath := cfg.DatabasePath()

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Build connection string with pragmas for better performance
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_cache_size=10000&_foreign_keys=ON",
		dbPath)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// SQLite specific optimizations
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=10000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	return db, nil
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

// IsSQLite returns true if using SQLite.
func (db *DB) IsSQLite() bool {
	return db.cfg.DatabaseType == config.DatabaseTypeSQLite
}

// HealthCheck verifies database connectivity.
func (db *DB) HealthCheck(ctx context.Context) error {
	return db.PingContext(ctx)
}
