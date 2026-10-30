//go:build integration

// Restore integration test against a throwaway MinIO and throwaway
// postgres:15-alpine and postgres:17-alpine app databases (the compose db
// is postgres 15). Every container it creates is named gfrs-it-* and
// removed afterwards; it never looks at other containers.
//
// Run with Docker and the PostgreSQL 17 client tools (pg_dump, pg_restore,
// psql) on PATH, the same client the runtime image ships:
//
//	go test -tags integration -run Integration_Restore ./internal/backup/
//
// GOFORGE_IT_PG_IMAGES overrides the server images (comma separated).
package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

const (
	rsAccessKey = "gfrsadmin"
	rsSecretKey = "gfrs-it-secret-key-0123456789" //nolint:gosec // throwaway MinIO credential, test only
	rsDBPass    = "gfrs-it-db-password-7c1e"      //nolint:gosec // throwaway postgres password, test only
	rsBucket    = "gfrs-it"
	rsPrefix    = "it/"
)

// getRecorder records every object key a restore reads.
type getRecorder struct {
	Store
	mu   sync.Mutex
	gets []string
}

func (g *getRecorder) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	g.mu.Lock()
	g.gets = append(g.gets, key)
	g.mu.Unlock()
	return g.Store.Get(ctx, key)
}

func TestIntegration_Restore(t *testing.T) {
	for _, bin := range []string{"docker", "pg_dump", "pg_restore", "psql"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	images := []string{"postgres:15-alpine", "postgres:17-alpine"}
	if v := os.Getenv("GOFORGE_IT_PG_IMAGES"); v != "" {
		images = strings.Split(v, ",")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)

	minioName := "gfrs-it-minio-" + suffix
	startContainer(t, minioName, "-e", "MINIO_ROOT_USER="+rsAccessKey, "-e", "MINIO_ROOT_PASSWORD="+rsSecretKey,
		"-p", "127.0.0.1::9000", itMinioImage, "server", "/data")
	endpoint := "127.0.0.1:" + strings.TrimPrefix(strings.Split(dockerCLI(t, "port", minioName, "9000"), "\n")[0], "127.0.0.1:")
	mc, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(rsAccessKey, rsSecretKey, ""), BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "minio", minioName, func() bool { _, lerr := mc.ListBuckets(ctx); return lerr == nil })
	if err := mc.MakeBucket(ctx, rsBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	bc := config.BackupConfig{Endpoint: "http://" + endpoint, Region: "us-east-1", Bucket: rsBucket,
		AccessKeyID: rsAccessKey, SecretAccessKey: rsSecretKey, ForcePathStyle: true}

	for i, img := range images {
		image := strings.TrimSpace(img)
		t.Run(image, func(t *testing.T) {
			bc := bc
			bc.Prefix = fmt.Sprintf("%s%d/", rsPrefix, i)
			testRestoreAgainst(t, image, "gfrs-it-pg"+fmt.Sprint(i)+"-"+suffix, bc)
		})
	}
}

func testRestoreAgainst(t *testing.T, image, pgName string, bc config.BackupConfig) {
	ctx := context.Background()
	startContainer(t, pgName, "-e", "POSTGRES_USER=goforge", "-e", "POSTGRES_PASSWORD="+rsDBPass, "-e", "POSTGRES_DB=goforge",
		"-p", "127.0.0.1::5432", image)
	port := strings.TrimPrefix(strings.Split(dockerCLI(t, "port", pgName, "5432"), "\n")[0], "127.0.0.1:")
	waitFor(t, "postgres", pgName, func() bool {
		return execOK(pgName, "psql", "-U", "goforge", "-d", "goforge", "-h", "127.0.0.1", "-c", "select 1")
	})
	dbURL := "postgres://goforge:" + rsDBPass + "@127.0.0.1:" + port + "/goforge?sslmode=disable"
	cfg := &config.Config{DatabaseType: config.DatabaseTypePostgres, DatabaseURL: dbURL}

	// A migrated GoForge database with an admin and a live session.
	migrate := func() {
		t.Helper()
		db, err := database.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := database.MigrateUp(db, cfg); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	migrate()
	exec1 := func(q string) {
		t.Helper()
		db := openDB(t, dbURL, "goforge")
		defer db.Close()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(dbName, q string, args ...any) int {
		t.Helper()
		db := openDB(t, dbURL, dbName)
		defer db.Close()
		var n int
		if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	exec1(`INSERT INTO users (email, password_hash) VALUES ('admin@example.test', 'x')`)
	exec1(`INSERT INTO sessions (user_id, token_hash, expires_at) SELECT id, 'tok-before', now() + interval '1 day' FROM users`)

	// Back up with the real Runner, app database only.
	red := NewRedactor(rsSecretKey, rsAccessKey, rsDBPass)
	s3, err := NewS3Store(bc)
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Store: s3, Runs: &memRuns{}, Prefix: bc.Prefix, Retention: 7, Location: time.UTC, Timeout: 5 * time.Minute,
		Version: "it", Redactor: red, Sources: []Source{Static{&AppDBTarget{DatabaseURL: dbURL, Redactor: red}}}}
	run, err := runner.Run(ctx, models.BackupTriggerManual)
	if err != nil || run.Status != models.BackupRunSucceeded {
		t.Fatalf("backup: %v %+v", err, run)
	}
	runKey := run.RunKey

	// Changes after the backup that a restore must undo.
	exec1(`INSERT INTO audit_logs (action, resource_type) VALUES ('it.after-backup', 'test')`)
	exec1(`INSERT INTO sessions (user_id, token_hash, expires_at) SELECT id, 'tok-after', now() + interval '1 day' FROM users`)
	afterMarker := `SELECT count(*) FROM audit_logs WHERE action = 'it.after-backup'`

	latest, err := database.LatestMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	var progress bytes.Buffer
	rec := &getRecorder{Store: s3}
	newRestorer := func() *Restorer {
		return &Restorer{Store: rec, Prefix: bc.Prefix, Bucket: bc.Bucket, DatabaseURL: dbURL, LatestMigration: latest,
			Redactor: NewRedactor(rsSecretKey, rsAccessKey, rsDBPass), Progress: &progress, TempDir: t.TempDir()}
	}
	sideGone := func() {
		t.Helper()
		if n := count("postgres", `SELECT count(*) FROM pg_database WHERE datname LIKE 'goforge\_restore\_%'`); n != 0 {
			t.Errorf("side database left behind (%d)", n)
		}
		if n := count("goforge", afterMarker); n != 1 {
			t.Errorf("live database changed by a refused restore")
		}
	}
	// copyRun copies the run to another run key, letting edit change the
	// manifest and dump.
	copyRun := func(newKey string, edit func(m map[string]any, dump []byte) []byte) {
		t.Helper()
		get := func(k string) []byte {
			rc, gerr := s3.Get(ctx, ObjectKey(bc.Prefix, runKey, k))
			if gerr != nil {
				t.Fatal(gerr)
			}
			defer rc.Close()
			b, rerr := io.ReadAll(rc)
			if rerr != nil {
				t.Fatal(rerr)
			}
			return b
		}
		var m map[string]any
		if uerr := json.Unmarshal(get(ManifestName), &m); uerr != nil {
			t.Fatal(uerr)
		}
		m["run_key"] = newKey
		dump := edit(m, get(AppDumpKey))
		body, merr := json.Marshal(m)
		if merr != nil {
			t.Fatal(merr)
		}
		if perr := s3.Put(ctx, ObjectKey(bc.Prefix, newKey, AppDumpKey), bytes.NewReader(dump), ""); perr != nil {
			t.Fatal(perr)
		}
		if perr := s3.Put(ctx, ObjectKey(bc.Prefix, newKey, ManifestName), bytes.NewReader(body), ""); perr != nil {
			t.Fatal(perr)
		}
	}
	appArtifact := func(m map[string]any) map[string]any {
		for _, a := range m["artifacts"].([]any) {
			if am := a.(map[string]any); am["key"] == AppDumpKey {
				return am
			}
		}
		t.Fatal("no app artifact")
		return nil
	}

	// Restore points list the run.
	points, _, err := RestorePoints(ctx, s3, bc.Prefix, 0)
	if err != nil || len(points) != 1 || points[0].RunKey != runKey || points[0].Manifest == nil {
		t.Fatalf("restore points = %+v, %v", points, err)
	}

	t.Run("refuse-no-manifest", func(t *testing.T) {
		if perr := s3.Put(ctx, ObjectKey(bc.Prefix, "2026-01-01/20260101T000000Z", AppDumpKey), strings.NewReader("x"), ""); perr != nil {
			t.Fatal(perr)
		}
		if _, perr := newRestorer().Plan(ctx, "2026-01-01/20260101T000000Z"); !errors.Is(perr, ErrRunIncomplete) {
			t.Fatalf("got %v", perr)
		}
	})
	t.Run("refuse-manifest-too-new", func(t *testing.T) {
		copyRun("2026-01-02/20260102T000000Z", func(m map[string]any, d []byte) []byte { m["version"] = ManifestVersion + 1; return d })
		if _, perr := newRestorer().Plan(ctx, "2026-01-02/20260102T000000Z"); !errors.Is(perr, ErrManifestTooNew) {
			t.Fatalf("got %v", perr)
		}
	})
	t.Run("refuse-sha-mismatch", func(t *testing.T) {
		copyRun("2026-01-03/20260103T000000Z", func(m map[string]any, d []byte) []byte {
			appArtifact(m)["sha256"] = strings.Repeat("0", 64)
			return d
		})
		r := newRestorer()
		plan, perr := r.Plan(ctx, "2026-01-03/20260103T000000Z")
		if perr != nil {
			t.Fatal(perr)
		}
		if _, rerr := r.Restore(ctx, plan); !errors.Is(rerr, ErrRestoreIntegrity) {
			t.Fatalf("got %v", rerr)
		}
		sideGone()
	})
	t.Run("refuse-bytes-mismatch", func(t *testing.T) {
		copyRun("2026-01-04/20260104T000000Z", func(m map[string]any, d []byte) []byte { return append(d, 0) })
		r := newRestorer()
		plan, perr := r.Plan(ctx, "2026-01-04/20260104T000000Z")
		if perr != nil {
			t.Fatal(perr)
		}
		if _, rerr := r.Restore(ctx, plan); !errors.Is(rerr, ErrRestoreIntegrity) {
			t.Fatalf("got %v", rerr)
		}
		sideGone()
	})

	r := newRestorer()
	plan, err := r.Plan(ctx, runKey)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("refuse-backup-lock-held", func(t *testing.T) {
		db := openDB(t, dbURL, "goforge")
		defer db.Close()
		conn, cerr := db.Conn(ctx)
		if cerr != nil {
			t.Fatal(cerr)
		}
		defer conn.Close()
		if _, lerr := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, repositories.BackupLockKey); lerr != nil {
			t.Fatal(lerr)
		}
		if perr := newRestorer().Preflight(ctx, plan); !errors.Is(perr, ErrRestoreBackupRunning) {
			t.Fatalf("preflight: %v", perr)
		}
		if _, rerr := newRestorer().Restore(ctx, plan); !errors.Is(rerr, ErrRestoreBackupRunning) {
			t.Fatalf("restore: %v", rerr)
		}
		sideGone()
	})
	t.Run("refuse-other-connection", func(t *testing.T) {
		db := openDBApp(t, dbURL, "goforge", "goforge-serve-it")
		defer db.Close()
		if perr := db.PingContext(ctx); perr != nil {
			t.Fatal(perr)
		}
		_, rerr := newRestorer().Restore(ctx, plan)
		if !errors.Is(rerr, ErrRestoreDatabaseInUse) || !strings.Contains(rerr.Error(), "docker compose stop goforge") ||
			!strings.Contains(rerr.Error(), "goforge-serve-it") {
			t.Fatalf("got %v", rerr)
		}
		sideGone()
	})
	t.Run("refuse-schema-newer", func(t *testing.T) {
		old := newRestorer()
		old.LatestMigration = latest - 1
		if _, rerr := old.Restore(ctx, plan); !errors.Is(rerr, ErrRestoreSchemaNewer) {
			t.Fatalf("got %v", rerr)
		}
		sideGone()
	})
	t.Run("refuse-broken-dump-commits-nothing", func(t *testing.T) {
		// A dump whose manifest matches but whose archive is truncated:
		// pg_restore fails part way and psql must not commit.
		copyRun("2026-01-05/20260105T000000Z", func(m map[string]any, d []byte) []byte {
			cut := d[:len(d)*2/3]
			a := appArtifact(m)
			a["bytes"] = len(cut)
			a["sha256"] = sha256Hex(cut)
			return cut
		})
		r := newRestorer()
		p, perr := r.Plan(ctx, "2026-01-05/20260105T000000Z")
		if perr != nil {
			t.Fatal(perr)
		}
		if _, rerr := r.Restore(ctx, p); rerr == nil {
			t.Fatal("restore of a truncated dump succeeded")
		} else {
			t.Logf("truncated dump refused: %v", rerr)
		}
		sideGone()
	})

	// The real restore.
	rec.mu.Lock()
	rec.gets = nil
	rec.mu.Unlock()
	res, err := newRestorer().Restore(ctx, plan)
	if err != nil {
		t.Fatalf("restore: %v\nprogress:\n%s", err, progress.String())
	}
	t.Logf("restored: previous=%s schema=%d sessions=%d warnings=%v", res.PreviousDB, res.SchemaVersion, res.SessionsCleared, res.Warnings)
	if res.PreviousDB == "" || !strings.HasPrefix(res.PreviousDB, "goforge_pre_restore_") {
		t.Fatalf("previous db = %q", res.PreviousDB)
	}
	if res.SchemaVersion != latest {
		t.Errorf("schema = %d, want %d", res.SchemaVersion, latest)
	}
	if res.SessionsCleared != 1 {
		t.Errorf("sessions cleared = %d, want 1 (the one in the dump)", res.SessionsCleared)
	}
	if n := count("goforge", afterMarker); n != 0 {
		t.Errorf("post-backup change survived the restore")
	}
	if n := count("goforge", `SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("sessions = %d, want 0", n)
	}
	if n := count("goforge", `SELECT count(*) FROM users WHERE email = 'admin@example.test'`); n != 1 {
		t.Errorf("admin missing after restore")
	}
	if n := count("goforge", `SELECT count(*) FROM audit_logs WHERE action = 'backup.restore' AND resource_type = 'backup_run' AND details = jsonb_build_object('run_key', $1::text)`, runKey); n != 1 {
		t.Errorf("backup.restore audit rows = %d, want 1", n)
	}
	if n := count(res.PreviousDB, afterMarker); n != 1 {
		t.Errorf("previous database lost the post-backup change")
	}
	if n := count(res.PreviousDB, `SELECT count(*) FROM sessions`); n != 2 {
		t.Errorf("previous database sessions = %d, want 2 (kept as it was)", n)
	}
	if n := count("postgres", `SELECT count(*) FROM pg_database WHERE datname LIKE 'goforge\_restore\_%'`); n != 0 {
		t.Errorf("side database left after success")
	}
	// Only the manifest and the app dump were read; never services/...
	for _, k := range rec.gets {
		if k != ObjectKey(bc.Prefix, runKey, ManifestName) && k != plan.ObjectKey {
			t.Errorf("restore read %s", k)
		}
	}
	for _, s := range []string{rsDBPass, rsSecretKey, rsAccessKey} {
		if strings.Contains(progress.String(), s) {
			t.Errorf("progress output contains a secret")
		}
	}
	// The next serve start migrates the restored database without error.
	migrate()
	t.Logf("progress:\n%s", progress.String())
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func openDB(t *testing.T, dbURL, dbName string) *sql.DB {
	t.Helper()
	return openDBApp(t, dbURL, dbName, "gfrs-it")
}

func openDBApp(t *testing.T, dbURL, dbName, app string) *sql.DB {
	t.Helper()
	dsn, err := dsnFor(dbURL, dbName)
	if err != nil {
		t.Fatal(err)
	}
	dsn = strings.Replace(dsn, "application_name="+restoreAppName, "application_name="+app, 1)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
