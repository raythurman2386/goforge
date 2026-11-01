package backup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Restore refusals. Each one is returned before the live database changes.
var (
	// ErrRestoreBackupRunning means the backup advisory lock is held: a
	// backup run (serve's scheduler or `goforge backup run`) is going.
	ErrRestoreBackupRunning = errors.New("a backup run holds the backup lock; wait for it to finish and stop goforge serve")
	// ErrRestoreDatabaseInUse means other clients are connected to the
	// GoForge database, normally goforge serve.
	ErrRestoreDatabaseInUse = errors.New("other clients are connected to the GoForge database; stop GoForge first: docker compose stop goforge")
	// ErrRestoreIntegrity means the downloaded dump does not match the
	// manifest's size or SHA-256.
	ErrRestoreIntegrity = errors.New("the downloaded dump does not match manifest.json")
	// ErrRestoreSchemaNewer means the dump's schema_migrations version is
	// newer than this binary's migrations.
	ErrRestoreSchemaNewer = errors.New("the backup was taken by a newer GoForge (its schema is newer than this binary); upgrade GoForge first")
	// ErrRestoreNotGoForge means the restored database is missing GoForge
	// tables.
	ErrRestoreNotGoForge = errors.New("the dump is not a GoForge database")
)

// restoreAppName is the application_name of every restore connection.
const restoreAppName = "goforge-restore"

// maintenanceDB is the database restore connects to for CREATE, DROP and
// RENAME DATABASE.
const maintenanceDB = "postgres"

// Restorer restores the GoForge app database from one backup run. It only
// reads <run>/manifest.json and <run>/app/goforge-db.dump; it never reads a
// services/... object and never touches a container.
//
// The dump is restored into a side database (<db>_restore_<run id>) with the
// local pg_restore converting the archive to SQL, then psql loading it with
// ON_ERROR_STOP in one transaction. Only after every check passes are the
// databases renamed: <db> becomes <db>_pre_restore_<UTC time> and the side
// database becomes <db>. The previous database is never dropped.
type Restorer struct {
	Store Store
	// Prefix is the normalized key prefix the run lives under.
	Prefix string
	// DatabaseURL is the GoForge DATABASE_URL (postgres://).
	DatabaseURL string
	// PgRestore and Psql are the client binaries; empty means PATH.
	PgRestore, Psql string
	// LatestMigration is the newest migration version this binary ships.
	LatestMigration uint
	// Bucket is the bucket the run was found in, for the settings warning.
	Bucket string
	// TempDir holds the downloaded dump (mode 600) until Restore returns.
	TempDir  string
	Redactor *Redactor
	// Progress gets one line per step. No secret is ever written to it.
	Progress io.Writer
	Now      func() time.Time
}

// RestorePlan is what a restore would do, from the manifest alone.
type RestorePlan struct {
	RunKey    string
	Manifest  *Manifest
	Dump      ManifestArtifact
	ObjectKey string
	// Database is the GoForge database name from DATABASE_URL.
	Database string
	SideDB   string
}

// RestoreResult is a finished restore.
type RestoreResult struct {
	Plan *RestorePlan
	// PreviousDB is the renamed pre-restore database, or "" when the GoForge
	// database did not exist before.
	PreviousDB      string
	SchemaVersion   uint
	SessionsCleared int64
	// Warnings are non-fatal findings (backup settings in the dump differ
	// from the ones that found the run).
	Warnings []string
}

func (r *Restorer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Restorer) progress(format string, args ...any) {
	if r.Progress != nil {
		fmt.Fprintf(r.Progress, format+"\n", args...)
	}
}

// Plan reads and checks the run's manifest and finds the app dump. It does
// not touch any database.
func (r *Restorer) Plan(ctx context.Context, runKey string) (*RestorePlan, error) {
	dbName, err := databaseName(r.DatabaseURL)
	if err != nil {
		return nil, err
	}
	m, err := ReadManifest(ctx, r.Store, r.Prefix, runKey)
	if err != nil {
		return nil, err
	}
	a := m.Artifact(AppDumpKey)
	if a == nil {
		return nil, fmt.Errorf("run %s has no %s in its manifest", runKey, AppDumpKey)
	}
	if a.Kind != KindPgDump {
		return nil, fmt.Errorf("%s has kind %q, want %q", AppDumpKey, a.Kind, KindPgDump)
	}
	side := dbName + "_restore_" + strings.ToLower(runKey[len(dateLayout)+1:])
	if len(side) > 63 {
		return nil, fmt.Errorf("database name %q is too long for a side database", dbName)
	}
	return &RestorePlan{
		RunKey: runKey, Manifest: m, Dump: *a,
		ObjectKey: ObjectKey(r.Prefix, runKey, AppDumpKey),
		Database:  dbName, SideDB: side,
	}, nil
}

// Preflight checks the live database can be replaced now: the backup lock
// is free and no other client is connected. It changes nothing. A database
// that does not exist yet passes.
func (r *Restorer) Preflight(ctx context.Context, plan *RestorePlan) error {
	lk, err := r.lockTarget(ctx, plan)
	if err != nil {
		return err
	}
	lk.release(ctx)
	return nil
}

// targetLock is the backup advisory lock held on one connection to the
// live database.
type targetLock struct {
	db   *sql.DB
	conn *sql.Conn
}

func (l *targetLock) release(ctx context.Context) {
	if l == nil || l.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _ = l.conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, repositories.BackupLockKey) //nolint:errcheck // closing the connection releases it anyway
	l.conn.Close()
	l.db.Close()
	l.conn = nil
}

// checkIdle refuses while another client backend is connected. A backend
// that is just disconnecting (this command's own settings read) gets a
// moment to go away first.
func (l *targetLock) checkIdle(ctx context.Context) error {
	var n int
	var apps string
	for attempt := 0; ; attempt++ {
		err := l.conn.QueryRowContext(ctx, `SELECT count(*), coalesce(string_agg(DISTINCT NULLIF(application_name, ''), ', '), '')
		FROM pg_stat_activity
		WHERE datname = current_database() AND pid <> pg_backend_pid() AND backend_type = 'client backend'`).Scan(&n, &apps)
		if err != nil {
			return fmt.Errorf("failed to list connections: %w", err)
		}
		if n == 0 || attempt == 3 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if n > 0 {
		if apps != "" {
			return fmt.Errorf("%w (%d connected: %s)", ErrRestoreDatabaseInUse, n, apps)
		}
		return fmt.Errorf("%w (%d connected)", ErrRestoreDatabaseInUse, n)
	}
	return nil
}

// lockTarget takes the backup lock on the live database and checks no
// other client is connected. It returns a nil lock when the database does
// not exist.
func (r *Restorer) lockTarget(ctx context.Context, plan *RestorePlan) (*targetLock, error) {
	exists, err := r.databaseExists(ctx, plan.Database)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	db, err := r.open(ctx, plan.Database)
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to %s: %s", plan.Database, r.Redactor.Redact(err.Error()))
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, repositories.BackupLockKey).Scan(&ok); err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("failed to take the backup lock: %w", err)
	}
	if !ok {
		conn.Close()
		db.Close()
		return nil, ErrRestoreBackupRunning
	}
	l := &targetLock{db: db, conn: conn}
	if err := l.checkIdle(ctx); err != nil {
		l.release(ctx)
		return nil, err
	}
	return l, nil
}

// Restore runs the plan. Every refusal (lock, connections, integrity,
// schema) happens before the live database is renamed; on any error the
// side database is dropped and the live one is left as it was.
func (r *Restorer) Restore(ctx context.Context, plan *RestorePlan) (*RestoreResult, error) {
	if r.LatestMigration == 0 {
		return nil, errors.New("restorer has no LatestMigration")
	}
	pgRestore, psql, err := r.clients()
	if err != nil {
		return nil, err
	}

	lock, err := r.lockTarget(ctx, plan)
	if err != nil {
		return nil, err
	}
	defer lock.release(ctx)
	if lock == nil {
		r.progress("database %s does not exist yet; it will be created from the backup", plan.Database)
	} else {
		r.progress("backup lock taken; no other client is connected to %s", plan.Database)
	}

	dumpPath, err := r.download(ctx, plan)
	if err != nil {
		return nil, err
	}
	defer os.Remove(dumpPath)

	maint, err := r.open(ctx, maintenanceDB)
	if err != nil {
		return nil, err
	}
	defer maint.Close()
	maint.SetMaxOpenConns(1)

	side := pq.QuoteIdentifier(plan.SideDB)
	exists, err := r.databaseExistsOn(ctx, maint, plan.SideDB)
	if err != nil {
		return nil, err
	}
	if exists {
		// Only this command creates <db>_restore_<run id>; a leftover is
		// from an attempt that failed before the swap.
		r.progress("dropping leftover side database %s from an earlier attempt", plan.SideDB)
		if _, err := maint.ExecContext(ctx, `DROP DATABASE `+side); err != nil {
			return nil, fmt.Errorf("failed to drop leftover %s: %w", plan.SideDB, err)
		}
	}
	if _, err := maint.ExecContext(ctx, `CREATE DATABASE `+side+` TEMPLATE template0`); err != nil {
		return nil, fmt.Errorf("failed to create %s (the DATABASE_URL role needs CREATEDB): %w", plan.SideDB, err)
	}
	swapped := false
	defer func() {
		if swapped {
			return
		}
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, derr := maint.ExecContext(dctx, `DROP DATABASE IF EXISTS `+side); derr != nil {
			r.progress("could not drop side database %s: %s; drop it by hand", plan.SideDB, r.Redactor.Redact(derr.Error()))
		} else {
			r.progress("dropped side database %s; %s is unchanged", plan.SideDB, plan.Database)
		}
	}()
	r.progress("created side database %s", plan.SideDB)

	if err := r.load(ctx, pgRestore, psql, dumpPath, plan.SideDB); err != nil {
		return nil, err
	}
	r.progress("restored the dump into %s", plan.SideDB)

	res := &RestoreResult{Plan: plan}
	if err := r.finishSide(ctx, plan, res); err != nil {
		return nil, err
	}

	// Swap. The lock connection is the last one on the live database; close
	// it, then rename both in one transaction. A client that connected in
	// between makes the rename fail, and nothing changes.
	if lock != nil {
		if err := lock.checkIdle(ctx); err != nil {
			return nil, err
		}
		lock.release(ctx)
		res.PreviousDB = plan.Database + "_pre_restore_" + strings.ToLower(r.now().UTC().Format(runIDLayout))
	}
	if err := r.swap(ctx, maint, plan, res.PreviousDB); err != nil {
		return nil, err
	}
	swapped = true
	if res.PreviousDB != "" {
		r.progress("renamed %s to %s (kept; drop it yourself when you no longer need it)", plan.Database, res.PreviousDB)
	}
	r.progress("renamed %s to %s", plan.SideDB, plan.Database)
	return res, nil
}

func (r *Restorer) clients() (pgRestore, psql string, err error) {
	pgRestore, psql = r.PgRestore, r.Psql
	if pgRestore == "" {
		if pgRestore, err = exec.LookPath("pg_restore"); err != nil {
			return "", "", errors.New("pg_restore not found on PATH; run restore in the GoForge container (it ships postgresql17-client)")
		}
	}
	if psql == "" {
		if psql, err = exec.LookPath("psql"); err != nil {
			return "", "", errors.New("psql not found on PATH; run restore in the GoForge container (it ships postgresql17-client)")
		}
	}
	return pgRestore, psql, nil
}

// download writes the dump to a mode 600 temp file and checks its size and
// SHA-256 against the manifest.
func (r *Restorer) download(ctx context.Context, plan *RestorePlan) (string, error) {
	path, n, err := fetchVerified(ctx, r.Store, plan.ObjectKey, plan.Dump, r.TempDir, r.Redactor)
	if err != nil {
		return "", err
	}
	r.progress("downloaded %s: %d bytes, SHA-256 matches the manifest", AppDumpKey, n)
	return path, nil
}

// fetchVerified downloads objectKey to a mode 600 temp file in dir and
// checks its size and SHA-256 against the manifest entry a. On any error the
// temp file is removed. Integrity failures wrap ErrRestoreIntegrity.
func fetchVerified(ctx context.Context, store Store, objectKey string, a ManifestArtifact, dir string, red *Redactor) (path string, n int64, err error) {
	rc, err := store.Get(ctx, objectKey)
	if errors.Is(err, ErrObjectNotFound) {
		return "", 0, fmt.Errorf("%w: %s is missing from the bucket", ErrRestoreIntegrity, a.Key)
	}
	if err != nil {
		return "", 0, fmt.Errorf("download %s: %s", a.Key, red.Redact(err.Error()))
	}
	defer rc.Close()
	f, err := os.CreateTemp(dir, "goforge-restore-*")
	if err != nil {
		return "", 0, fmt.Errorf("failed to create a temp file: %w", err)
	}
	path = f.Name()
	h := sha256.New()
	n, cerr := io.Copy(io.MultiWriter(f, h), rc)
	if closeErr := f.Close(); cerr == nil {
		cerr = closeErr
	}
	if cerr != nil {
		os.Remove(path)
		return "", 0, fmt.Errorf("download %s: %s", a.Key, red.Redact(cerr.Error()))
	}
	sum := hex.EncodeToString(h.Sum(nil))
	switch {
	case n != a.Bytes:
		os.Remove(path)
		return "", 0, fmt.Errorf("%w: %s is %d bytes, manifest says %d", ErrRestoreIntegrity, a.Key, n, a.Bytes)
	case sum != a.SHA256:
		os.Remove(path)
		return "", 0, fmt.Errorf("%w: %s SHA-256 is %s, manifest says %s", ErrRestoreIntegrity, a.Key, sum, a.SHA256)
	}
	return path, n, nil
}

// load runs pg_restore --file=- | filterRestoreSQL | psql --single-transaction
// into db. pg_restore only converts the archive to SQL, so it needs no
// connection; psql gets the libpq environment from DATABASE_URL, never argv.
func (r *Restorer) load(ctx context.Context, pgRestore, psql, dumpPath, db string) error {
	env, err := pgEnvFromURL(r.DatabaseURL)
	if err != nil {
		return err
	}
	env = setEnv(env, "PGDATABASE", db)
	env = setEnv(env, "PGAPPNAME", restoreAppName)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	rs := exec.CommandContext(ctx, pgRestore, "--no-password", "--file=-", dumpPath)
	rs.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	rsErr := &bytes.Buffer{}
	rs.Stderr = &limitedWriter{w: rsErr, n: 16 << 10}
	out, err := rs.StdoutPipe()
	if err != nil {
		return err
	}
	ps := exec.CommandContext(ctx, psql, "--no-psqlrc", "--quiet", "--no-password",
		"--set=ON_ERROR_STOP=1", "--single-transaction")
	ps.Env = env
	ps.Stdout = io.Discard
	psErr := &bytes.Buffer{}
	ps.Stderr = &limitedWriter{w: psErr, n: 16 << 10}
	in, err := ps.StdinPipe()
	if err != nil {
		return err
	}
	if err := ps.Start(); err != nil {
		return fmt.Errorf("psql: %w", err)
	}
	if err := rs.Start(); err != nil {
		cancel()
		in.Close()
		_ = ps.Wait() //nolint:errcheck // the start error is the one to report
		return fmt.Errorf("pg_restore: %w", err)
	}
	ferr := filterRestoreSQL(out, in)
	if ferr != nil {
		// psql stopped reading (it failed) or the pipe broke: stop
		// pg_restore too so Wait cannot block on a full pipe.
		cancel()
	}
	rerr := rs.Wait()
	if ferr != nil || rerr != nil {
		// Never let psql see a clean end of input after a failed
		// pg_restore: it would commit a partial script. Kill it first.
		cancel()
		perr := ps.Wait()
		in.Close()
		if ferr != nil && perr != nil && psErr.Len() > 0 {
			return fmt.Errorf("psql: %w: %s", perr, r.Redactor.Redact(psErr.String()))
		}
		if rerr != nil && ferr == nil {
			return fmt.Errorf("pg_restore: %w: %s", rerr, r.Redactor.Redact(rsErr.String()))
		}
		return fmt.Errorf("restore pipe: %w", errors.Join(ferr, rerr))
	}
	in.Close()
	if err := ps.Wait(); err != nil {
		return fmt.Errorf("psql: %w: %s", err, r.Redactor.Redact(psErr.String()))
	}
	return nil
}

// finishSide checks the restored side database and prepares it to go live:
// GoForge tables present, schema not newer than this binary, sessions
// cleared, and a backup.restore audit row with the run key.
func (r *Restorer) finishSide(ctx context.Context, plan *RestorePlan, res *RestoreResult) error {
	db, err := r.open(ctx, plan.SideDB)
	if err != nil {
		return err
	}
	defer db.Close()
	for _, t := range []string{"users", "sessions", "audit_logs", "schema_migrations"} {
		var reg sql.NullString
		if qerr := db.QueryRowContext(ctx, `SELECT to_regclass('public.' || $1)::text`, t).Scan(&reg); qerr != nil {
			return fmt.Errorf("check table %s: %w", t, qerr)
		}
		if !reg.Valid {
			return fmt.Errorf("%w: table %s is missing", ErrRestoreNotGoForge, t)
		}
	}
	var version int64
	var dirty bool
	if qerr := db.QueryRowContext(ctx, `SELECT version, dirty FROM public.schema_migrations LIMIT 1`).Scan(&version, &dirty); qerr != nil {
		return fmt.Errorf("%w: read schema_migrations: %w", ErrRestoreNotGoForge, qerr)
	}
	switch {
	case version < 0 || uint64(version) > uint64(r.LatestMigration):
		return fmt.Errorf("%w (backup schema %d, this binary knows up to %d)", ErrRestoreSchemaNewer, version, r.LatestMigration)
	case dirty:
		return fmt.Errorf("the backup's schema_migrations is dirty at version %d; it was taken mid-migration", version)
	}
	res.SchemaVersion = uint(version)
	r.progress("schema_migrations version %d (this binary: %d)", version, r.LatestMigration)

	warn, err := r.settingsWarnings(ctx, db)
	if err != nil {
		return err
	}
	res.Warnings = append(res.Warnings, warn...)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit
	del, err := tx.ExecContext(ctx, `DELETE FROM public.sessions`)
	if err != nil {
		return fmt.Errorf("clear sessions: %w", err)
	}
	res.SessionsCleared, _ = del.RowsAffected() //nolint:errcheck // lib/pq always reports it
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO public.audit_logs (action, resource_type, details) VALUES ($1, $2, jsonb_build_object('run_key', $3::text))`,
		models.AuditActionBackupRestore, models.ResourceTypeBackupRun, plan.RunKey); err != nil {
		return fmt.Errorf("write audit row: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.progress("cleared %d sessions; wrote audit row %s", res.SessionsCleared, models.AuditActionBackupRestore)
	return nil
}

// settingsWarnings compares the dump's saved bucket and prefix with the
// ones that found the run. The dump's row is kept either way.
func (r *Restorer) settingsWarnings(ctx context.Context, db *sql.DB) ([]string, error) {
	var reg sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.backup_settings')::text`).Scan(&reg); err != nil {
		return nil, err
	}
	if !reg.Valid {
		return nil, nil
	}
	var bucket, prefix string
	err := db.QueryRowContext(ctx, `SELECT bucket, prefix FROM public.backup_settings WHERE id = 1`).Scan(&bucket, &prefix)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return []string{"the backup has no saved backup settings; goforge serve will use BACKUP_* from the environment"}, nil
	case err != nil:
		return nil, fmt.Errorf("read backup settings: %w", err)
	}
	var out []string
	if r.Bucket != "" && bucket != r.Bucket {
		out = append(out, fmt.Sprintf("the backup's saved bucket %q differs from %q, where this run was found; check Settings → Profile → Backups", bucket, r.Bucket))
	}
	if prefix != r.Prefix {
		out = append(out, fmt.Sprintf("the backup's saved prefix %q differs from %q, where this run was found; check Settings → Profile → Backups", prefix, r.Prefix))
	}
	return out, nil
}

func (r *Restorer) swap(ctx context.Context, maint *sql.DB, plan *RestorePlan, previous string) error {
	tx, err := maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a no-op after Commit
	if previous != "" {
		if _, err := tx.ExecContext(ctx, `ALTER DATABASE `+pq.QuoteIdentifier(plan.Database)+` RENAME TO `+pq.QuoteIdentifier(previous)); err != nil {
			return fmt.Errorf("rename %s to %s: %w (if a client connected, stop it and run restore again)", plan.Database, previous, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER DATABASE `+pq.QuoteIdentifier(plan.SideDB)+` RENAME TO `+pq.QuoteIdentifier(plan.Database)); err != nil {
		return fmt.Errorf("rename %s to %s: %w", plan.SideDB, plan.Database, err)
	}
	return tx.Commit()
}

func (r *Restorer) databaseExists(ctx context.Context, name string) (bool, error) {
	maint, err := r.open(ctx, maintenanceDB)
	if err != nil {
		return false, err
	}
	defer maint.Close()
	return r.databaseExistsOn(ctx, maint, name)
}

func (r *Restorer) databaseExistsOn(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname = $1`, name).Scan(&n); err != nil {
		return false, fmt.Errorf("failed to read pg_database: %s", r.Redactor.Redact(err.Error()))
	}
	return n > 0, nil
}

// open connects to dbName on the DATABASE_URL server.
func (r *Restorer) open(ctx context.Context, dbName string) (*sql.DB, error) {
	dsn, err := dsnFor(r.DatabaseURL, dbName)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %s", dbName, r.Redactor.Redact(err.Error()))
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to %s: %s", dbName, r.Redactor.Redact(err.Error()))
	}
	return db, nil
}

// parseDatabaseURL parses a postgres:// or postgresql:// DATABASE_URL. The
// error never includes the URL.
func parseDatabaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != maintenanceDB && u.Scheme != "postgresql") {
		return nil, errors.New("DATABASE_URL must be a postgres:// URL for restore")
	}
	return u, nil
}

// databaseName is the database in a postgres:// URL.
func databaseName(raw string) (string, error) {
	u, err := parseDatabaseURL(raw)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(u.Path, "/")
	switch {
	case name == "":
		return "", errors.New("DATABASE_URL has no database name")
	case name == maintenanceDB || strings.HasPrefix(name, "template"):
		return "", fmt.Errorf("refusing to restore over the %q database", name)
	}
	return name, nil
}

// dsnFor is DATABASE_URL pointed at dbName, with sslmode defaulted the way
// database.New does and the restore application_name.
func dsnFor(raw, dbName string) (string, error) {
	u, err := parseDatabaseURL(raw)
	if err != nil {
		return "", err
	}
	u.Path = "/" + dbName
	u.RawPath = ""
	q := u.Query()
	if q.Get("sslmode") == "" {
		q.Set("sslmode", "disable")
	}
	q.Set("application_name", restoreAppName)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func setEnv(env []string, key, val string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+val)
}

// filterRestoreSQL copies pg_restore's SQL script, dropping
// "SET transaction_timeout" lines outside COPY data. The PostgreSQL 17
// client writes that setting, and a PostgreSQL 15 or 16 server rejects it,
// which would stop psql under ON_ERROR_STOP.
func filterRestoreSQL(src io.Reader, dst io.Writer) error {
	br := bufio.NewReaderSize(src, 1<<20)
	bw := bufio.NewWriterSize(dst, 1<<20)
	inCopy := false
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := bytes.TrimRight(line, "\r\n")
			skip := false
			switch {
			case inCopy:
				if bytes.Equal(trimmed, []byte(`\.`)) {
					inCopy = false
				}
			case bytes.HasPrefix(line, []byte("COPY ")) && bytes.HasSuffix(trimmed, []byte("FROM stdin;")):
				inCopy = true
			case bytes.HasPrefix(line, []byte("SET transaction_timeout")):
				skip = true
			}
			if !skip {
				if _, werr := bw.Write(line); werr != nil {
					return werr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return bw.Flush()
		}
		if err != nil {
			return err
		}
	}
}
