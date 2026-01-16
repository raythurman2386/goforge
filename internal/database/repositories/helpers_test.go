package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
)

func setupSharedTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}

	cfg := &config.Config{
		DatabaseType: config.DatabaseTypePostgres,
		DatabaseURL:  dsn,
	}

	goforgeDB, err := database.New(cfg)
	if err != nil {
		t.Skipf("skipping test; failed to connect to postgres: %v", err)
	}

	if err := database.MigrateUp(goforgeDB, cfg); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Truncate all tables to ensure clean state
	tables := []string{
		"users", "sessions", "projects", "environments", "deployments",
		"service_instances", "containers", "env_variables", "git_sources",
		"audit_logs", "deployment_logs",
	}

	for _, table := range tables {
		_, err := goforgeDB.ExecContext(context.Background(), fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			// Ignore error if table doesn't exist (e.g. before migrations run properly? No, migrations ran.)
			// But maybe migrations failed silently? No.
			// Just log error.
			t.Logf("failed to truncate table %s: %v", table, err)
		}
	}

	return goforgeDB.DB
}
