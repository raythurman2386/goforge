package repositories

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/secrets"
)

var (
	testDB     *sql.DB
	testDBOnce sync.Once
	errTestDB  error

	// testCounter provides unique identifiers for test data to avoid conflicts.
	testCounter uint64

	// testRunID provides a unique prefix for this test run to avoid conflicts
	// with data from previous test runs.
	testRunID = fmt.Sprintf("%d", time.Now().UnixNano())
)

// TestMain sets up the shared test database once for all tests.
func TestMain(m *testing.M) {
	code := m.Run()
	// Cleanup
	if testDB != nil {
		testDB.Close()
	}
	os.Exit(code)
}

// getTestDB returns a shared database connection for all tests.
// The database is initialized once and reused across all tests.
func getTestDB(t *testing.T) *sql.DB {
	t.Helper()

	testDBOnce.Do(func() {
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
			errTestDB = fmt.Errorf("failed to connect to postgres: %w", err)
			return
		}

		if err := database.MigrateUp(goforgeDB, cfg); err != nil {
			goforgeDB.Close()
			errTestDB = fmt.Errorf("failed to run migrations: %w", err)
			return
		}

		testDB = goforgeDB.DB
	})

	if errTestDB != nil {
		t.Skipf("skipping test; %v", errTestDB)
	}

	return testDB
}

// uniqueID returns a unique identifier for test data.
// This allows tests to run without truncating tables between tests.
// Uses a combination of test run ID and counter to ensure uniqueness across runs.
func uniqueID() string {
	return fmt.Sprintf("%s-%d", testRunID, atomic.AddUint64(&testCounter, 1))
}

// uniqueEmail returns a unique email for test users.
func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@test.local", prefix, uniqueID())
}

// testSecretService creates a secrets service for testing.
var (
	testSecretService     *secrets.EncryptionService
	testSecretServiceOnce sync.Once
)

func getTestSecretService(t *testing.T) *secrets.EncryptionService {
	t.Helper()

	testSecretServiceOnce.Do(func() {
		key, _ := secrets.GenerateKeyString(32)
		cfg := &config.Config{EncryptionKey: key}
		testSecretService, _ = secrets.NewEncryptionService(cfg)
	})

	return testSecretService
}
