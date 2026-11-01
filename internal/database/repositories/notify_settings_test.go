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
func TestNotifySettingsRepository_SealedRoundTrip(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	wipe := func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM notify_settings`); err != nil {
			t.Fatal(err)
		}
	}
	wipe()
	t.Cleanup(wipe)

	repo := NewNotifySettingsRepository(db, getTestSecretService(t))
	if _, err := repo.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty table = %v, want ErrNotFound", err)
	}

	const topic, tok = "https://ntfy.example.test/topic-roundtrip-secret", "tk-roundtrip-secret"
	in := &models.NotifySettings{Enabled: true, Kind: models.NotifyKindNtfy, URL: topic, Token: tok}
	if err := repo.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	var urlCol, tokCol string
	if err := db.QueryRowContext(ctx, `SELECT url_sealed, token_sealed FROM notify_settings`).Scan(&urlCol, &tokCol); err != nil {
		t.Fatal(err)
	}
	if urlCol == "" || tokCol == "" || strings.Contains(urlCol, "topic-roundtrip") || strings.Contains(tokCol, tok) {
		t.Fatal("url and token columns are not ciphertext")
	}
	got, err := repo.Get(ctx)
	if err != nil || !got.Enabled || got.Kind != models.NotifyKindNtfy || got.URL != topic || got.Token != tok {
		t.Fatalf("round trip: %+v %v", got, err)
	}

	got.Kind, got.Token, got.Enabled = models.NotifyKindWebhook, "", false
	if err = repo.Upsert(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, err := repo.Get(ctx)
	if err != nil || got2.Enabled || got2.Kind != models.NotifyKindWebhook || got2.Token != "" || got2.URL != topic {
		t.Fatalf("upsert: %+v %v", got2, err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notify_settings`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d %v, want 1", n, err)
	}

	// The CHECK rejects an unknown kind.
	if _, err = db.ExecContext(ctx, `UPDATE notify_settings SET kind = 'email'`); err == nil {
		t.Fatal("kind 'email' accepted")
	}

	// A different ENCRYPTION_KEY cannot read the URL; the rest comes back.
	if err = repo.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	key, _ := secrets.GenerateKeyString(32)
	other, _ := secrets.NewEncryptorFromString(key)
	partial, err := NewNotifySettingsRepository(db, other).Get(ctx)
	if !errors.Is(err, ErrNotifySettingsUnreadable) || partial == nil || partial.URL != "" || partial.Token != "" || !partial.Enabled || partial.Kind != models.NotifyKindNtfy {
		t.Fatalf("Get with another key = %+v, %v", partial, err)
	}
	if err := NewNotifySettingsRepository(db, nil).Upsert(ctx, in); err == nil {
		t.Fatal("Upsert without an encryptor stored the URL")
	}
}
