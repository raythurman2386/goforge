// Package database provides database connection and migration management.
package database

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/user/gocool/internal/config"
)

//go:embed migrations/postgres/*.sql
var postgresFS embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteFS embed.FS

// Migrator handles database migrations.
type Migrator struct {
	db      *sql.DB
	dbType  config.DatabaseType
	migrate *migrate.Migrate
}

// NewMigrator creates a new migration manager.
func NewMigrator(db *DB) (*Migrator, error) {
	m := &Migrator{
		db:     db.DB,
		dbType: db.cfg.DatabaseType,
	}

	if err := m.init(); err != nil {
		return nil, err
	}

	return m, nil
}

// init initializes the migrate instance.
func (m *Migrator) init() error {
	var driver database.Driver
	var err error
	var sourceURL string

	switch m.dbType {
	case config.DatabaseTypePostgres:
		driver, err = postgres.WithInstance(m.db, &postgres.Config{})
		if err != nil {
			return fmt.Errorf("failed to create postgres driver: %w", err)
		}
		sourceURL = "migrations/postgres"

		source, err := iofs.New(postgresFS, sourceURL)
		if err != nil {
			return fmt.Errorf("failed to create migration source: %w", err)
		}

		m.migrate, err = migrate.NewWithInstance("iofs", source, "postgres", driver)
		if err != nil {
			return fmt.Errorf("failed to create migrator: %w", err)
		}

	case config.DatabaseTypeSQLite:
		driver, err = sqlite3.WithInstance(m.db, &sqlite3.Config{})
		if err != nil {
			return fmt.Errorf("failed to create sqlite driver: %w", err)
		}
		sourceURL = "migrations/sqlite"

		source, err := iofs.New(sqliteFS, sourceURL)
		if err != nil {
			return fmt.Errorf("failed to create migration source: %w", err)
		}

		m.migrate, err = migrate.NewWithInstance("iofs", source, "sqlite3", driver)
		if err != nil {
			return fmt.Errorf("failed to create migrator: %w", err)
		}

	default:
		return fmt.Errorf("unsupported database type: %s", m.dbType)
	}

	return nil
}

// Up runs all pending migrations.
func (m *Migrator) Up() error {
	if err := m.migrate.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration up failed: %w", err)
	}
	return nil
}

// Down rolls back the last migration.
func (m *Migrator) Down() error {
	if err := m.migrate.Steps(-1); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration down failed: %w", err)
	}
	return nil
}

// Reset rolls back all migrations and re-applies them.
func (m *Migrator) Reset() error {
	if err := m.migrate.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration reset (down) failed: %w", err)
	}
	if err := m.migrate.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration reset (up) failed: %w", err)
	}
	return nil
}

// Version returns the current migration version.
func (m *Migrator) Version() (uint, bool, error) {
	return m.migrate.Version()
}

// Steps migrates up or down by the given number of steps.
func (m *Migrator) Steps(n int) error {
	if err := m.migrate.Steps(n); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migration steps failed: %w", err)
	}
	return nil
}

// Force sets the migration version without running migrations.
// Use with caution - this is for fixing broken migration state.
func (m *Migrator) Force(version int) error {
	return m.migrate.Force(version)
}

// Close closes the migration resources.
func (m *Migrator) Close() error {
	sourceErr, dbErr := m.migrate.Close()
	if sourceErr != nil {
		return sourceErr
	}
	return dbErr
}
