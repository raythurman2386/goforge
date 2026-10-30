package repositories

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

// The table holds one row, so this test owns it: it is not parallel and
// leaves the table empty.
func TestBackupSettingsRepository_SealedRoundTrip(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	wipe := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM backup_settings`); err != nil {
			t.Fatal(err)
		}
	}
	wipe()
	t.Cleanup(wipe)

	repo := NewBackupSettingsRepository(db, getTestSecretService(t))
	if _, err := repo.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty table = %v, want ErrNotFound", err)
	}

	const ak, sk = "AKIA-roundtrip-0001", "sk-roundtrip-secret-value-xyz"
	in := &models.BackupSettings{
		Enabled: true, Endpoint: "https://s3.example.test", Region: "auto", Bucket: "b1", Prefix: "goforge/",
		AccessKeyID: ak, SecretAccessKey: sk, ForcePathStyle: true,
		Schedule: "03:15 America/Chicago", RetentionDaily: 9, Timeout: 90 * time.Minute,
	}
	wrote, err := repo.InsertIfAbsent(ctx, in)
	if err != nil || !wrote {
		t.Fatalf("InsertIfAbsent = %v, %v; want true, nil", wrote, err)
	}
	again := *in
	again.Bucket = "other"
	if wrote, err = repo.InsertIfAbsent(ctx, &again); err != nil || wrote {
		t.Fatalf("second InsertIfAbsent = %v, %v; want false, nil", wrote, err)
	}

	var akCol, skCol string
	if scanErr := db.QueryRowContext(ctx, `SELECT access_key_id_sealed, secret_access_key_sealed FROM backup_settings`).Scan(&akCol, &skCol); scanErr != nil {
		t.Fatal(err)
	}
	if akCol == "" || skCol == "" || strings.Contains(akCol, ak) || strings.Contains(skCol, sk) {
		t.Fatalf("key columns are not ciphertext")
	}

	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bucket != "b1" || got.AccessKeyID != ak || got.SecretAccessKey != sk || got.Timeout != 90*time.Minute ||
		got.RetentionDaily != 9 || !got.ForcePathStyle || !got.Enabled || got.Schedule != "03:15 America/Chicago" {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	got.Enabled = false
	got.SecretAccessKey = ""
	if upErr := repo.Upsert(ctx, got); upErr != nil {
		t.Fatal(err)
	}
	got2, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Enabled || got2.SecretAccessKey != "" || got2.Bucket != "b1" {
		t.Fatalf("upsert mismatch: %+v", got2)
	}

	// A different ENCRYPTION_KEY cannot read the keys.
	if err := repo.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	key, _ := secrets.GenerateKeyString(32)
	other, _ := secrets.NewEncryptorFromString(key)
	if _, err := NewBackupSettingsRepository(db, other).Get(ctx); !errors.Is(err, ErrBackupSettingsUnreadable) {
		t.Fatalf("Get with another key = %v, want ErrBackupSettingsUnreadable", err)
	}
	if err := NewBackupSettingsRepository(db, nil).Upsert(ctx, in); err == nil {
		t.Fatal("Upsert without an encryptor stored keys")
	}
}
