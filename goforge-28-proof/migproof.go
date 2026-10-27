// One-off proof for migration 015. Not part of the application.
// MIG_DSN must point at an empty database.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
)

func main() {
	dsn := os.Getenv("MIG_DSN")
	if dsn == "" {
		fatalf("MIG_DSN is empty")
	}
	cfg := &config.Config{
		DatabaseType: config.DatabaseTypePostgres,
		DatabaseURL:  dsn,
	}
	db, err := database.New(cfg)
	if err != nil {
		fatalf("connect: %v", err)
	}
	defer db.Close()

	m, err := database.NewMigrator(db)
	if err != nil {
		fatalf("migrator: %v", err)
	}
	defer m.Close()

	if err := m.Steps(14); err != nil {
		fatalf("steps to 14: %v", err)
	}
	printVersion(m, "at-14")

	// Current 007 already checks for "canceled". Databases that applied the
	// older 007 still require "cancelled". Put that check back, then let 015
	// rewrite the row.
	if _, err := db.Exec(`ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_status_check`); err != nil {
		fatalf("drop check: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE deployments ADD CONSTRAINT deployments_status_check
		CHECK (status IN ('pending', 'cloning', 'building', 'deploying', 'running', 'failed', 'rolled_back', 'cancelled'))`); err != nil {
		fatalf("old check: %v", err)
	}

	var envID string
	err = db.QueryRow(`
		WITH u AS (
			INSERT INTO users (email, password_hash)
			VALUES ('mig-proof@example.com', 'x')
			RETURNING id
		),
		p AS (
			INSERT INTO projects (user_id, name, repository_url)
			SELECT id, 'mig', 'https://example.com/mig.git' FROM u
			RETURNING id
		),
		e AS (
			INSERT INTO environments (project_id, name)
			SELECT id, 'production' FROM p
			RETURNING id
		)
		INSERT INTO deployments (environment_id, status, trigger_type)
		SELECT id, 'cancelled', 'manual' FROM e
		RETURNING environment_id
	`).Scan(&envID)
	if err != nil {
		fatalf("insert cancelled row: %v", err)
	}

	var before string
	if err := db.QueryRow(`SELECT status FROM deployments`).Scan(&before); err != nil {
		fatalf("status before: %v", err)
	}
	fmt.Printf("status-before=%s\n", before)

	if err := m.Steps(1); err != nil {
		fatalf("step to 15: %v", err)
	}
	printVersion(m, "at-15")
	var afterUp string
	if err := db.QueryRow(`SELECT status FROM deployments`).Scan(&afterUp); err != nil {
		fatalf("status after up: %v", err)
	}
	fmt.Printf("status-after-up=%s\n", afterUp)

	if err := m.Steps(-1); err != nil {
		fatalf("step down to 14: %v", err)
	}
	printVersion(m, "at-14-down")
	var afterDown string
	if err := db.QueryRow(`SELECT status FROM deployments`).Scan(&afterDown); err != nil {
		fatalf("status after down: %v", err)
	}
	fmt.Printf("status-after-down=%s\n", afterDown)

	if before != "cancelled" || afterUp != "canceled" || afterDown != "cancelled" {
		os.Exit(1)
	}
}

func printVersion(m *database.Migrator, label string) {
	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		fatalf("version %s: %v", label, err)
	}
	fmt.Printf("version-%s=%d dirty=%v err=%v\n", label, v, dirty, err)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// Silence unused import if sql is needed only for side effects of database.New.
var _ = sql.ErrNoRows
