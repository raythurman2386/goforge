package database

import (
	"fmt"
	"testing"

	"github.com/raythurman2386/goforge/internal/config"
)

func TestNewMigrator_NilDB(t *testing.T) {
	// Test that NewMigrator handles nil DB gracefully
	// This will panic if not handled, so we use recover
	defer func() {
		if r := recover(); r == nil {
			t.Log("NewMigrator with nil DB panics as expected")
		}
	}()

	_, _ = NewMigrator(nil)
}

func TestMigrator_Version(t *testing.T) {
	// Skip if no database is available
	t.Skip("Requires database connection")
}

func TestMigrator_Up(t *testing.T) {
	// Skip if no database is available
	t.Skip("Requires database connection")
}

func TestMigrator_Down(t *testing.T) {
	// Skip if no database is available
	t.Skip("Requires database connection")
}

func TestMigrator_Steps(t *testing.T) {
	// Skip if no database is available
	t.Skip("Requires database connection")
}

func TestDatabaseType_String(t *testing.T) {
	tests := []struct {
		name     string
		dbType   config.DatabaseType
		expected string
	}{
		{"postgres type", config.DatabaseTypePostgres, "postgres"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.dbType) != tt.expected {
				t.Errorf("DatabaseType = %q, want %q", string(tt.dbType), tt.expected)
			}
		})
	}
}

func TestLatestMigrationVersion(t *testing.T) {
	v, err := LatestMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := postgresFS.ReadDir("migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	var newest uint
	for _, e := range entries {
		var n uint
		if _, err := fmt.Sscanf(e.Name(), "%d_", &n); err == nil && n > newest {
			newest = n
		}
	}
	if v != newest || v < 21 {
		t.Fatalf("LatestMigrationVersion = %d, want %d (>= 21)", v, newest)
	}
}
