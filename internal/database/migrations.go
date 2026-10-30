// Package database provides database connection and migration management.
package database

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/raythurman2386/goforge/internal/config"
)

//go:embed migrations/postgres/*.sql
var postgresFS embed.FS

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
	var src source.Driver

	switch m.dbType {
	case config.DatabaseTypePostgres:
		driver, err = postgres.WithInstance(m.db, &postgres.Config{})
		if err != nil {
			return fmt.Errorf("failed to create postgres driver: %w", err)
		}
		sourceURL = "migrations/postgres"

		src, err = iofs.New(postgresFS, sourceURL)
		if err != nil {
			return fmt.Errorf("failed to create migration source: %w", err)
		}

		m.migrate, err = migrate.NewWithInstance("iofs", src, "postgres", driver)
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
	if err := m.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration up failed: %w", err)
	}
	return nil
}

// Down rolls back the last migration.
func (m *Migrator) Down() error {
	if err := m.migrate.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration down failed: %w", err)
	}
	return nil
}

// Reset rolls back all migrations and re-applies them.
func (m *Migrator) Reset() error {
	if err := m.migrate.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration reset (down) failed: %w", err)
	}
	if err := m.migrate.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration reset (up) failed: %w", err)
	}
	return nil
}

// Version returns the current migration version.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	return m.migrate.Version()
}

// Steps migrates up or down by the given number of steps.
func (m *Migrator) Steps(n int) error {
	if err := m.migrate.Steps(n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration steps failed: %w", err)
	}
	return nil
}

// Force sets the migration version without running migrations.
// Use with caution - this is for fixing broken migration state.
func (m *Migrator) Force(version int) error {
	return m.migrate.Force(version)
}

// MigrateUp is a helper function that runs migrations for a database.
func MigrateUp(db *DB, cfg *config.Config) error {
	migrator, err := NewMigrator(db)
	if err != nil {
		return err
	}

	return migrator.Up()
}

// LatestMigrationVersion is the newest migration this binary ships. A
// database whose schema_migrations version is above it was written by a
// newer GoForge.
func LatestMigrationVersion() (uint, error) {
	src, err := iofs.New(postgresFS, "migrations/postgres")
	if err != nil {
		return 0, fmt.Errorf("failed to read migrations: %w", err)
	}
	defer src.Close()
	v, err := src.First()
	if err != nil {
		return 0, fmt.Errorf("failed to read migrations: %w", err)
	}
	for {
		next, err := src.Next(v)
		if err != nil {
			// fs.ErrNotExist (wrapped as *fs.PathError) marks the last one.
			return v, nil
		}
		v = next
	}
}

// Close closes the migration resources.
func (m *Migrator) Close() error {
	sourceErr, dbErr := m.migrate.Close()
	if sourceErr != nil {
		return sourceErr
	}
	return dbErr
}
