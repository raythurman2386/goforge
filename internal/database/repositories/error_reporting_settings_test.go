package repositories

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// The table holds one row, so this test owns it: it is not parallel and
// leaves the table empty.
func TestErrorReportingSettingsRepository_SealedRoundTrip(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	wipe := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM error_reporting_settings`); err != nil {
			t.Fatal(err)
		}
	}
	wipe()
	t.Cleanup(wipe)

	repo := NewErrorReportingSettingsRepository(db, getTestSecretService(t))
	if _, err := repo.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty table = %v, want ErrNotFound", err)
	}

	const dsn = "https://pubkey-roundtrip-secret@o1.ingest.sentry.example.test/42"
	in := &models.ErrorReportingSettings{DSN: dsn, Environment: "staging"}
	if err := repo.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	var col string
	if err := db.QueryRowContext(ctx, `SELECT dsn_sealed FROM error_reporting_settings`).Scan(&col); err != nil {
		t.Fatal(err)
	}
	if col == "" || strings.Contains(col, "pubkey-roundtrip") || strings.Contains(col, "@") {
		t.Fatal("dsn_sealed is not ciphertext")
	}
	got, err := repo.Get(ctx)
	if err != nil || got.DSN != dsn || got.Environment != "staging" {
		t.Fatalf("round trip: %+v %v", got, err)
	}

	// Blank DSN stores '' (error reporting off) and keeps one row.
	got.DSN = ""
	if err = repo.Upsert(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, err := repo.Get(ctx)
	if err != nil || got2.DSN != "" || got2.Environment != "staging" {
		t.Fatalf("blank upsert: %+v %v", got2, err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM error_reporting_settings`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d %v, want 1", n, err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO error_reporting_settings (id) VALUES (2)`); err == nil {
		t.Fatal("a second row was accepted")
	}

	// A different ENCRYPTION_KEY cannot read the DSN; the rest comes back.
	if err = repo.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	key, _ := secrets.GenerateKeyString(32)
	other, _ := secrets.NewEncryptorFromString(key)
	partial, err := NewErrorReportingSettingsRepository(db, other).Get(ctx)
	if !errors.Is(err, ErrErrorReportingSettingsUnreadable) || partial == nil || partial.DSN != "" || partial.Environment != "staging" {
		t.Fatalf("Get with another key = %+v, %v", partial, err)
	}
	if err := NewErrorReportingSettingsRepository(db, nil).Upsert(ctx, in); err == nil {
		t.Fatal("Upsert without an encryptor stored the DSN")
	}
}
