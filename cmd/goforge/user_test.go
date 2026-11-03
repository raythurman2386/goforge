package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/secrets"
)

type cliAudit struct {
	mu   sync.Mutex
	rows []string
	via  []interface{}
}

func (a *cliAudit) RecordAs(_ context.Context, _, _, action, _, _ string, d map[string]interface{}, _, _ string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rows = append(a.rows, action)
	a.via = append(a.via, d["via"])
}

// goforge user reset-2fa removes the enrollment, recovery codes, and pending
// challenges and writes user.2fa.reset (via cli). Needs DATABASE_URL.
func TestReset2FA(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	db, err := database.New(cfg)
	if err != nil {
		t.Skipf("skipping; no database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err = database.MigrateUp(db, cfg); err != nil {
		t.Skipf("skipping; migrate: %v", err)
	}
	ctx := context.Background()
	key, _ := secrets.GenerateKeyString(32)
	enc, err := secrets.NewEncryptorFromString(key)
	if err != nil {
		t.Fatal(err)
	}
	users := repositories.NewUserRepository(db.DB, nil)
	u := &models.User{Email: "reset-" + uuid.NewString() + "@example.com", PasswordHash: sql.NullString{String: "x", Valid: true}}
	if err = users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	tf := auth.NewTwoFactor(repositories.NewTwoFactorRepository(db.DB, enc))
	en, err := tf.BeginEnrollment(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := auth.TOTPCode(en.Secret, auth.TOTPStep(time.Now()))
	if _, err := tf.ConfirmEnrollment(ctx, u.ID, code); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tf.StartChallenge(ctx, u.ID, false); err != nil {
		t.Fatal(err)
	}

	audit := &cliAudit{}
	var out bytes.Buffer
	// The CLI uses a repository without ENCRYPTION_KEY; delete needs none.
	cliTF := auth.NewTwoFactor(repositories.NewTwoFactorRepository(db.DB, nil))
	if err := reset2FA(ctx, &out, strings.ToUpper(u.Email), users, cliTF, audit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "is off for") {
		t.Errorf("output = %q", out.String())
	}
	if len(audit.rows) != 1 || audit.rows[0] != models.AuditActionUser2FAReset || audit.via[0] != models.AuditViaCLI {
		t.Fatalf("audit = %v %v", audit.rows, audit.via)
	}
	var left int
	if err := db.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM user_totp WHERE user_id = $1) + (SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1) + (SELECT COUNT(*) FROM login_challenges WHERE user_id = $1)`, u.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left = %d (%v)", left, err)
	}

	// Again: nothing to do, no audit row.
	out.Reset()
	if err := reset2FA(ctx, &out, u.Email, users, cliTF, audit); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing changed") || len(audit.rows) != 1 {
		t.Fatalf("second reset: %q, %d rows", out.String(), len(audit.rows))
	}
	if err := reset2FA(ctx, &out, "nobody-"+uuid.NewString()+"@example.com", users, cliTF, audit); err == nil {
		t.Fatal("unknown email did not fail")
	}
}
