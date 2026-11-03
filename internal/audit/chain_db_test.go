package audit

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
)

func testAdminDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://goforge:password@localhost:5432/goforge?sslmode=disable"
	}
	return dsn
}

func withDatabase(t *testing.T, dsn, name string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// openThrowaway creates a private database, migrates it, and drops it when
// the test ends. It does not use the shared dev database, so a purge cannot
// delete rows another package inserted.
func openThrowaway(t *testing.T, name string) *sql.DB {
	t.Helper()
	adminDSN := testAdminDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if admin, err := sql.Open("postgres", adminDSN); err != nil {
		t.Skipf("postgres open: %v", err)
	} else if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		t.Skipf("postgres ping: %v", err)
	} else {
		_ = admin.Close()
	}
	// CREATE DATABASE needs a superuser. CI sets GOFORGE_TEST_SUPERUSER_DSN.
	// Without it, a local sudo -u postgres is the fallback.
	psqlSuper := func(sql string) error {
		return execSuperSQL(t, "", sql)
	}
	if err := psqlSuper(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`); err != nil {
		t.Skipf("superuser drop %s: %v", name, err)
	}
	if err := psqlSuper(`CREATE DATABASE ` + name + ` OWNER goforge`); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = psqlSuper(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
	})

	dsn := withDatabase(t, adminDSN, name)
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dsn}
	gdb, err := database.New(cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() { _ = gdb.Close() })
	if err := database.MigrateUp(gdb, cfg); err != nil {
		t.Fatalf("migrate %s: %v", name, err)
	}
	return gdb.DB
}

// execSuperSQL runs statement as a superuser. dbName empty uses the DSN's
// own database (for CREATE DATABASE). GOFORGE_TEST_SUPERUSER_DSN is the
// CI path. Unset, the local peer superuser via sudo is used.
func execSuperSQL(t *testing.T, dbName, statement string) error {
	t.Helper()
	if dsn := os.Getenv("GOFORGE_TEST_SUPERUSER_DSN"); dsn != "" {
		if dbName != "" {
			dsn = withDatabase(t, dsn, dbName)
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		_, err = db.Exec(statement)
		return err
	}
	args := []string{"-u", "postgres", "psql", "-v", "ON_ERROR_STOP=1"}
	if dbName != "" {
		args = append(args, "-d", dbName)
	}
	args = append(args, "-c", statement)
	cmd := exec.Command("sudo", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmtErr(string(out), err)
	}
	return nil
}

type cmdError struct{ msg string }

func (e cmdError) Error() string { return e.msg }

func fmtErr(out string, err error) error {
	out = strings.TrimSpace(out)
	if out == "" {
		return err
	}
	return cmdError{msg: out}
}

func TestMigration031_DownAndUp(t *testing.T) {
	db := openThrowaway(t, "goforge_audit_down")
	ctx := context.Background()
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: withDatabase(t, testAdminDSN(t), "goforge_audit_down")}
	fresh, err := database.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close() })
	m, err := database.NewMigrator(fresh)
	if err != nil {
		t.Fatal(err)
	}
	version, dirty, verr := m.Version()
	if verr != nil || dirty || version != 31 {
		t.Fatalf("version after up = %d dirty=%v err=%v", version, dirty, verr)
	}
	if downErr := m.Down(); downErr != nil {
		t.Fatal(downErr)
	}
	var settings sql.NullString
	if scanErr := db.QueryRowContext(ctx, `SELECT to_regclass('public.audit_settings')::text`).Scan(&settings); scanErr != nil {
		t.Fatal(scanErr)
	}
	if settings.Valid {
		t.Fatalf("audit_settings still present after down: %s", settings.String)
	}
	var fk int
	if scanErr := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint WHERE conname = 'audit_logs_user_id_fkey'`).Scan(&fk); scanErr != nil {
		t.Fatal(scanErr)
	}
	if fk != 1 {
		t.Fatalf("user_id fk count = %d", fk)
	}
	if upErr := m.Up(); upErr != nil {
		t.Fatal(upErr)
	}
	version, dirty, verr = m.Version()
	if verr != nil || dirty || version != 31 {
		t.Fatalf("version after re-up = %d dirty=%v err=%v", version, dirty, verr)
	}
}

func TestChain_TriggersSealPurgeAndTamper(t *testing.T) {
	db := openThrowaway(t, "goforge_audit_chain")
	ctx := context.Background()
	key, keyErr := DeriveChainKey("test-encryption-key-32-bytes-xx")
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	chain := NewChain(db, key, nil)

	assertRejects := func(op, want string) {
		t.Helper()
		_, execErr := db.ExecContext(ctx, op)
		if execErr == nil || !strings.Contains(execErr.Error(), want) {
			t.Fatalf("%s err = %v, want %q", op, execErr, want)
		}
	}
	assertRejects(`TRUNCATE audit_logs`, "append-only")
	assertRejects(`TRUNCATE audit_log_seals`, "append-only")

	var userID string
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, "audit-chain-"+t.Name()+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var freshID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, details)
		VALUES ($1, 'login', 'user', '{}') RETURNING id`, userID).Scan(&freshID); err != nil {
		t.Fatal(err)
	}
	assertRejects(`UPDATE audit_logs SET action = 'x' WHERE id = '`+freshID+`'`, "append-only")
	assertRejects(`DELETE FROM audit_logs WHERE id = '`+freshID+`'`, "append-only")

	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		t.Fatal(txErr)
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('goforge.audit_purge', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audit_logs SET action = 'x' WHERE id = $1`, freshID); err == nil || !strings.Contains(err.Error(), "updates are not allowed") {
		t.Fatalf("update under GUC err = %v", err)
	}
	_ = tx.Rollback()

	tx, txErr = db.BeginTx(ctx, nil)
	if txErr != nil {
		t.Fatal(txErr)
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('goforge.audit_purge', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM audit_logs WHERE id = $1`, freshID); err == nil || !strings.Contains(err.Error(), "newer than 30 days") {
		t.Fatalf("delete young under GUC err = %v", err)
	}
	_ = tx.Rollback()

	// An unsealed row older than the verifier window fails the check, and
	// retention then removes it because it was never part of the chain.
	var staleID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, details, created_at)
		VALUES ($1, 'stale', 'user', '{"approval_ref":"stale","idempotency_key":"stale"}', NOW() - INTERVAL '40 days')
		RETURNING id`, userID).Scan(&staleID); err != nil {
		t.Fatal(err)
	}
	if _, _, fail, err := chain.Verify(ctx, 0); err != nil || fail == nil || fail.Reason != ReasonUnsealed {
		t.Fatalf("unsealed verify = %+v %v", fail, err)
	}
	n, purgeErr := chain.PurgeOnce(ctx, 30)
	if purgeErr != nil || n != 1 {
		t.Fatalf("purge unsealed = %d %v", n, purgeErr)
	}
	if _, _, fail, err := chain.Verify(ctx, 0); err != nil || fail != nil {
		t.Fatalf("verify after unsealed purge = %+v %v", fail, err)
	}

	oldRef, youngRef := "old-ref", "young-ref"
	var oldID, youngID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, details, created_at)
		VALUES ($1, 'api_v1.restart_service', 'service_instance', $2::jsonb, NOW() - INTERVAL '40 days')
		RETURNING id`, userID, `{"approval_ref":"`+oldRef+`","idempotency_key":"k-old"}`).Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO audit_logs (user_id, action, resource_type, details, created_at)
		VALUES ($1, 'api_v1.restart_service', 'service_instance', $2::jsonb, NOW() - INTERVAL '10 days')
		RETURNING id`, userID, `{"approval_ref":"`+youngRef+`","idempotency_key":"k-young"}`).Scan(&youngID); err != nil {
		t.Fatal(err)
	}
	sealed, sealErr := chain.SealOnce(ctx)
	if sealErr != nil || sealed < 2 {
		t.Fatalf("sealed = %d %v", sealed, sealErr)
	}
	if _, _, fail, err := chain.Verify(ctx, 0); err != nil || fail != nil {
		t.Fatalf("verify before purge = %+v %v", fail, err)
	}
	n, purgeErr = chain.PurgeOnce(ctx, 30)
	if purgeErr != nil || n < 1 {
		t.Fatalf("purge = %d %v", n, purgeErr)
	}
	repo := repositories.NewAuditLogRepository(db)
	old, err := repo.FindApproval(ctx, userID, "api_v1.restart_service", oldRef, "k-old")
	if err != nil || old != nil {
		t.Fatalf("old approval still present: %+v %v", old, err)
	}
	young, youngErr := repo.FindApproval(ctx, userID, "api_v1.restart_service", youngRef, "k-young")
	if youngErr != nil || young == nil {
		t.Fatalf("young approval missing: %v", youngErr)
	}
	var latestHash string
	var latestSeq, latestDeleted int64
	if err := db.QueryRowContext(ctx, `
		SELECT (details->>'through_seq')::bigint, details->>'through_hash', (details->>'deleted')::bigint
		FROM audit_logs WHERE action = 'audit.retention.purge'
		ORDER BY created_at DESC, id DESC LIMIT 1`).Scan(&latestSeq, &latestHash, &latestDeleted); err != nil {
		t.Fatal(err)
	}
	if latestHash == "" || latestDeleted < 1 || latestSeq < 1 {
		t.Fatalf("purge row seq=%d hash=%s deleted=%d", latestSeq, latestHash, latestDeleted)
	}
	// The MAC on the purge row is the boundary proof. Verify must pass
	// before the sealer has sealed that row.
	headSeq, headHash, fail, verifyErr := chain.Verify(ctx, 0)
	if verifyErr != nil || fail != nil {
		t.Fatalf("verify across purge = %+v %v", fail, verifyErr)
	}
	if headSeq < 1 || headHash == nil {
		t.Fatalf("head = %d %x", headSeq, headHash)
	}

	// session_replication_role is superuser-only. CI sets
	// GOFORGE_TEST_SUPERUSER_DSN (the Compose Postgres user is superuser).
	// A local run without that variable uses sudo -u postgres.
	if _, err := uuid.Parse(youngID); err != nil {
		t.Fatal(err)
	}
	super := func(sql string) {
		t.Helper()
		if err := execSuperSQL(t, "goforge_audit_chain", sql); err != nil {
			t.Fatalf("superuser sql: %v", err)
		}
	}
	super(fmt.Sprintf(`SET session_replication_role = replica; UPDATE audit_logs SET action = 'tampered' WHERE id = '%s'`, youngID))
	_, _, fail, verifyErr = chain.Verify(ctx, 0)
	if verifyErr != nil || fail == nil || fail.Reason != ReasonHashMismatch || fail.Seq < 1 {
		t.Fatalf("tamper verify = %+v %v", fail, verifyErr)
	}
	super(fmt.Sprintf(`SET session_replication_role = replica; UPDATE audit_logs SET action = 'api_v1.restart_service' WHERE id = '%s'`, youngID))
	if _, _, restored, restoredErr := chain.Verify(ctx, 0); restoredErr != nil || restored != nil {
		t.Fatalf("verify after restore = %+v %v", restored, restoredErr)
	}
	super(fmt.Sprintf(`SET session_replication_role = replica; DELETE FROM audit_logs WHERE id = '%s'`, youngID))
	_, _, fail, verifyErr = chain.Verify(ctx, 0)
	if verifyErr != nil || fail == nil || fail.Reason != ReasonMissingRow {
		t.Fatalf("missing verify = %+v %v", fail, verifyErr)
	}
}

// TestVerify_BoundaryWithoutPurge is the Gatekeeper B1 attack: the app role
// sets goforge.audit_purge, deletes a prefix older than 30 days, and copies
// the last deleted seal into audit_settings. No purge row is written. The
// remaining head is unchanged, and verify must still exit 1.
func TestVerify_BoundaryWithoutPurge(t *testing.T) {
	db := openThrowaway(t, "goforge_audit_b1")
	ctx := context.Background()
	key, keyErr := DeriveChainKey("boundary-without-purge-key")
	if keyErr != nil {
		t.Fatal(keyErr)
	}
	chain := NewChain(db, key, nil)
	for i, age := range []string{"40 days", "38 days", "36 days", "1 day"} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO audit_logs (action, resource_type, details, created_at)
			VALUES ('keep', 'user', $1::jsonb, NOW() - $2::interval)`,
			fmt.Sprintf(`{"n":%d}`, i), age); err != nil {
			t.Fatal(err)
		}
	}
	sealed, sealErr := chain.SealOnce(ctx)
	if sealErr != nil || sealed != 4 {
		t.Fatalf("sealed = %d %v", sealed, sealErr)
	}
	if _, _, fail, err := chain.Verify(ctx, 0); err != nil || fail != nil {
		t.Fatalf("verify before attack = %+v %v", fail, err)
	}
	var boundarySeq int64
	var boundaryHash []byte
	if err := db.QueryRowContext(ctx, `
		SELECT seq, row_hash FROM audit_log_seals WHERE seq = 3`).Scan(&boundarySeq, &boundaryHash); err != nil {
		t.Fatal(err)
	}
	var headBefore int64
	var headHash []byte
	if err := db.QueryRowContext(ctx, `
		SELECT seq, row_hash FROM audit_log_seals ORDER BY seq DESC LIMIT 1`).Scan(&headBefore, &headHash); err != nil {
		t.Fatal(err)
	}
	tx, txErr := db.BeginTx(ctx, nil)
	if txErr != nil {
		t.Fatal(txErr)
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('goforge.audit_purge', 'on', true)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM audit_log_seals WHERE seq <= 3`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM audit_logs WHERE created_at < NOW() - INTERVAL '30 days'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_settings (id, boundary_seq, boundary_hash)
		VALUES (1, $1, $2)
		ON CONFLICT (id) DO UPDATE SET
			boundary_seq = EXCLUDED.boundary_seq,
			boundary_hash = EXCLUDED.boundary_hash`, boundarySeq, boundaryHash); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var headAfter int64
	var headHashAfter []byte
	if err := db.QueryRowContext(ctx, `
		SELECT seq, row_hash FROM audit_log_seals ORDER BY seq DESC LIMIT 1`).Scan(&headAfter, &headHashAfter); err != nil {
		t.Fatal(err)
	}
	if headAfter != headBefore || !bytes.Equal(headHash, headHashAfter) {
		t.Fatalf("head changed %d %x -> %d %x", headBefore, headHash, headAfter, headHashAfter)
	}
	var purgeRows int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs WHERE action = 'audit.retention.purge'`).Scan(&purgeRows); err != nil {
		t.Fatal(err)
	}
	if purgeRows != 0 {
		t.Fatalf("purge rows = %d, want 0", purgeRows)
	}
	_, _, fail, verifyErr := chain.Verify(ctx, 0)
	if verifyErr != nil || fail == nil || fail.Reason != ReasonBoundaryPurge || fail.Seq != boundarySeq {
		t.Fatalf("forged boundary verify = %+v %v", fail, verifyErr)
	}

	// B1′: insert a matching purge row with a bogus MAC and let SealOnce
	// seal it. The head may move. Verify must still fail.
	forged := fmt.Sprintf(
		`{"deleted":3,"cutoff":"2026-10-04T00:00:00Z","through_seq":%d,"through_hash":"%s","purge_mac":"%s"}`,
		boundarySeq, hexEncode(boundaryHash), "0000000000000000000000000000000000000000000000000000000000000000")
	if _, execErr := db.ExecContext(ctx, `
		INSERT INTO audit_logs (action, resource_type, details, created_at)
		VALUES ('audit.retention.purge', 'audit_settings', $1::jsonb, NOW() - INTERVAL '6 seconds')`,
		forged); execErr != nil {
		t.Fatal(execErr)
	}
	sealedForged, forgedSealErr := chain.SealOnce(ctx)
	if forgedSealErr != nil || sealedForged < 1 {
		t.Fatalf("seal forged purge = %d %v", sealedForged, forgedSealErr)
	}
	var headAfterSeal int64
	if scanErr := db.QueryRowContext(ctx, `SELECT MAX(seq) FROM audit_log_seals`).Scan(&headAfterSeal); scanErr != nil {
		t.Fatal(scanErr)
	}
	if headAfterSeal <= headAfter {
		t.Fatalf("head after seal = %d, want > %d", headAfterSeal, headAfter)
	}
	_, _, fail, verifyErr = chain.Verify(ctx, 0)
	if verifyErr != nil || fail == nil || fail.Reason != ReasonBoundaryPurge || fail.Seq != boundarySeq {
		t.Fatalf("forged sealed purge verify = %+v %v", fail, verifyErr)
	}
}
