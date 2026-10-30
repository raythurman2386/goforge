package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/secrets"
)

var backupListCmd = &cobra.Command{
	Use:   "list",
	Short: "List restore points: complete backup runs in the bucket, newest first",
	Long: `List complete backup runs (the ones with a manifest.json) under the
backup prefix, newest first. Uses the same backup settings as "goforge serve":
the saved Settings → Profile → Backups row when one exists, else BACKUP_*.
Times are shown in the local time zone.`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupList(cmd.Context(), cmd.OutOrStdout())
	},
}

var (
	restoreYes     bool
	restoreConfirm string
)

var backupRestoreCmd = &cobra.Command{
	Use:   "restore <run-key>",
	Short: "Restore the GoForge database from a backup run (dry run unless --yes)",
	Long: `Restore the GoForge app database (app/goforge-db.dump) from one backup run.
Service databases and volumes are not restored and no container is touched.

Without --yes this is a dry run: it reads the manifest, checks that GoForge
is stopped, prints what would happen, and changes nothing.

With --yes it downloads the dump, checks its size and SHA-256, restores it
into a side database, checks the schema is not newer than this binary,
clears sessions, writes a backup.restore audit row, then renames the current
database to <db>_pre_restore_<UTC time> (kept, never dropped) and the side
database to <db>. Confirm by typing the run key at the prompt, or pass
--confirm=<run-key> when there is no terminal.

Stop GoForge first and run it in a one-off container:

  docker compose stop goforge
  docker compose run --rm --no-deps goforge ./goforge backup restore <run-key> --yes
  docker compose start goforge`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupRestore(cmd.Context(), args[0], cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

func init() {
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupRestoreCmd)
	backupRestoreCmd.Flags().BoolVar(&restoreYes, "yes", false, "restore for real (without it, a dry run)")
	backupRestoreCmd.Flags().StringVar(&restoreConfirm, "confirm", "", "the run key again, to confirm without a terminal prompt")
}

// backupTarget is the bucket a list or restore reads, and the redactor that
// covers every secret in play.
type backupTarget struct {
	cfg      *config.Config
	bc       config.BackupConfig
	source   string
	store    *backup.S3Store
	redactor *backup.Redactor
}

// loadBackupTarget resolves the backup settings the way serve does. When
// the GoForge database is missing or has no backup_settings table (a fresh
// server being restored), it falls back to BACKUP_*.
func loadBackupTarget(ctx context.Context, out io.Writer) (*backupTarget, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	red := backup.RedactorFor(cfg)
	bc, source, err := effectiveBackupConfig(ctx, cfg, out)
	if err != nil {
		return nil, errors.New(red.Redact(err.Error()))
	}
	if strings.TrimSpace(bc.Bucket) == "" {
		return nil, errors.New("no backup bucket is configured: set BACKUP_* or save Settings → Profile → Backups")
	}
	// A paused schedule (Disabled) still names the bucket to read from.
	bc.Disabled = false
	red = backup.RedactorFor(cfg, bc)
	store, err := backup.NewS3Store(bc)
	if err != nil {
		return nil, errors.New(red.Redact(err.Error()))
	}
	return &backupTarget{cfg: cfg, bc: bc, source: source, store: store, redactor: red}, nil
}

func effectiveBackupConfig(ctx context.Context, cfg *config.Config, out io.Writer) (config.BackupConfig, string, error) {
	envOnly := func(why string) (config.BackupConfig, string, error) {
		if cfg.BackupEnvErr != nil {
			return config.BackupConfig{}, "", fmt.Errorf("invalid backup configuration: %w", cfg.BackupEnvErr)
		}
		fmt.Fprintf(out, "note: %s; using BACKUP_* from the environment\n", why)
		return cfg.Backup, backup.SourceEnvironment, nil
	}
	db, err := database.New(cfg) //nolint:contextcheck // database.New takes no context
	if err != nil {
		return envOnly("the GoForge database is not reachable")
	}
	defer db.Close()
	var reg *string
	if qerr := db.QueryRowContext(ctx, `SELECT to_regclass('public.backup_settings')::text`).Scan(&reg); qerr != nil || reg == nil {
		return envOnly("the GoForge database has no backup settings table")
	}
	enc, err := secrets.NewEncryptorFromString(cfg.EncryptionKey)
	if err != nil {
		return config.BackupConfig{}, "", fmt.Errorf("failed to create encryptor: %w", err)
	}
	store := repositories.NewBackupSettingsRepository(db.DB, enc)
	if ignored, cerr := backup.CheckEnv(ctx, store, cfg.BackupEnvErr); cerr != nil {
		return config.BackupConfig{}, "", fmt.Errorf("invalid backup configuration: %w", cerr)
	} else if ignored {
		fmt.Fprint(out, "note: BACKUP_* in the environment is invalid and ignored because saved backup settings exist\n")
	}
	bc, source, err := backup.Effective(ctx, store, cfg.Backup)
	if errors.Is(err, repositories.ErrBackupSettingsUnreadable) {
		return config.BackupConfig{}, "", fmt.Errorf("read backup settings: %w; an admin must enter both keys again in Settings → Profile → Backups", err)
	}
	if err != nil {
		return config.BackupConfig{}, "", fmt.Errorf("read backup settings: %w", err)
	}
	return bc, source, nil
}

func runBackupList(ctx context.Context, out io.Writer) error {
	t, err := loadBackupTarget(ctx, out)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	points, incomplete, err := backup.RestorePoints(ctx, t.store, t.bc.Prefix, 0)
	if err != nil {
		return errors.New(t.redactor.Redact(err.Error()))
	}
	fmt.Fprintf(out, "Restore points in s3://%s/%s (settings: %s), newest first:\n", t.bc.Bucket, t.bc.Prefix, t.source)
	writeRestorePoints(out, points, time.Local)
	if len(points) == 0 {
		fmt.Fprint(out, "(none)\n")
	}
	if incomplete > 0 {
		fmt.Fprintf(out, "%d incomplete run(s) without manifest.json are not listed; they cannot be restored.\n", incomplete)
	}
	return nil
}

func writeRestorePoints(out io.Writer, points []backup.RestorePoint, loc *time.Location) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "RUN KEY\tTRIGGER\tGOFORGE\tSTARTED\tARTIFACTS\tBYTES\tSKIPPED\n")
	for _, p := range points {
		if p.Manifest == nil {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\t-\t(%s)\n", p.RunKey, p.Problem)
			continue
		}
		m := p.Manifest
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%d\n", p.RunKey, m.Trigger, m.GoForgeVersion,
			m.StartedAt.In(loc).Format("2006-01-02 15:04:05 MST"), len(m.Artifacts), m.TotalBytes(), len(m.Skipped))
	}
	tw.Flush()
}

func runBackupRestore(ctx context.Context, runKey string, in io.Reader, out io.Writer) error {
	if !backup.ValidRunKey(runKey) {
		return fmt.Errorf("%w: %q (want YYYY-MM-DD/YYYYMMDDTHHMMSSZ, as printed by goforge backup list)", backup.ErrBadRunKey, runKey)
	}
	if restoreConfirm != "" && !restoreYes {
		return errors.New("--confirm only takes effect with --yes")
	}
	t, err := loadBackupTarget(ctx, out)
	if err != nil {
		return err
	}
	latest, err := database.LatestMigrationVersion()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := &backup.Restorer{
		Store: t.store, Prefix: t.bc.Prefix, Bucket: t.bc.Bucket,
		DatabaseURL: t.cfg.DatabaseURL, LatestMigration: latest,
		Redactor: t.redactor, Progress: out,
	}
	plan, err := r.Plan(ctx, runKey)
	if err != nil {
		return errors.New(t.redactor.Redact(err.Error()))
	}
	preErr := r.Preflight(ctx, plan)
	printRestorePlan(out, t, plan, latest, preErr)

	if !restoreYes {
		fmt.Fprintf(out, "\nDry run: nothing changed. To restore, run:\n  goforge backup restore %s --yes\n", runKey)
		if preErr != nil {
			return errors.New("restore would be refused: " + t.redactor.Redact(preErr.Error()))
		}
		return nil
	}
	if preErr != nil {
		return errors.New(t.redactor.Redact(preErr.Error()))
	}
	if cerr := confirmRestore(runKey, in, out); cerr != nil {
		return cerr
	}
	fmt.Fprint(out, "\n")
	res, err := r.Restore(ctx, plan)
	if err != nil {
		return errors.New("restore failed, the GoForge database is unchanged: " + t.redactor.Redact(err.Error()))
	}
	printRestoreResult(out, plan, res)
	return nil
}

func printRestorePlan(out io.Writer, t *backupTarget, plan *backup.RestorePlan, latest uint, preErr error) {
	m := plan.Manifest
	started := m.StartedAt.In(time.Local).Format("2006-01-02 15:04:05 MST")
	fmt.Fprint(out, "Restore plan\n")
	fmt.Fprintf(out, "  run:           %s (s3://%s/%s%s/)\n", plan.RunKey, t.bc.Bucket, t.bc.Prefix, plan.RunKey)
	fmt.Fprintf(out, "  taken:         %s, trigger %s, GoForge %s (this binary: %s, schema %d)\n", started, m.Trigger, m.GoForgeVersion, version, latest)
	fmt.Fprintf(out, "  app dump:      %s, %d bytes, sha256 %s\n", plan.Dump.Key, plan.Dump.Bytes, plan.Dump.SHA256)
	fmt.Fprintf(out, "  restores into: %s, then renames %s to %s_pre_restore_<UTC time> (kept) and %s to %s\n",
		plan.SideDB, plan.Database, plan.Database, plan.SideDB, plan.Database)
	fmt.Fprintf(out, "  replaces:      every GoForge table (users, API tokens, projects, env vars, service records, backup settings, audit log) with its state at %s; all sessions are cleared\n", started)
	if n := len(m.Artifacts) - 1; n > 0 {
		fmt.Fprintf(out, "  not restored:  %d service artifact(s) under services/; containers and volumes are not touched (see docs/backup.md for the manual runbooks)\n", n)
	}
	if preErr != nil {
		fmt.Fprintf(out, "  preflight:     REFUSED: %s\n", t.redactor.Redact(preErr.Error()))
	} else {
		fmt.Fprint(out, "  preflight:     ok (backup lock free, no other client connected)\n")
	}
}

func confirmRestore(runKey string, in io.Reader, out io.Writer) error {
	if restoreConfirm != "" {
		if restoreConfirm != runKey {
			return fmt.Errorf("--confirm=%q does not match the run key %s", restoreConfirm, runKey)
		}
		return nil
	}
	f, ok := in.(*os.File)
	if !ok {
		return errors.New("no terminal to confirm on: pass --confirm=<run-key>")
	}
	// A character device is not enough: /dev/null is one too (docker run without -t).
	if !isatty.IsTerminal(f.Fd()) {
		return errors.New("no terminal to confirm on: pass --confirm=<run-key> (or use docker compose run with a TTY)")
	}
	fmt.Fprintf(out, "\nThis replaces the GoForge database. Type the run key to confirm: ")
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(line) != runKey {
		return errors.New("the typed run key does not match; nothing changed")
	}
	return nil
}

func printRestoreResult(out io.Writer, plan *backup.RestorePlan, res *backup.RestoreResult) {
	started := plan.Manifest.StartedAt.In(time.Local).Format("2006-01-02 15:04:05 MST")
	fmt.Fprintf(out, "\nRestored %s from %s (taken %s).\n", plan.Database, plan.RunKey, started)
	if res.PreviousDB != "" {
		fmt.Fprintf(out, "The previous database is kept as %s. It is never dropped automatically.\n", res.PreviousDB)
	}
	fmt.Fprintf(out, "Schema version %d; goforge serve runs any newer migrations when it starts.\n", res.SchemaVersion)
	fmt.Fprintf(out, "Cleared %d session(s): everyone signs in again. API tokens are as they were at %s.\n", res.SessionsCleared, started)
	for _, w := range res.Warnings {
		fmt.Fprintf(out, "warning: %s\n", w)
	}
	fmt.Fprint(out, "\nNext:\n")
	fmt.Fprint(out, "  - ENCRYPTION_KEY must be the one in use when the backup was taken, or saved env vars and backup keys will not decrypt.\n")
	fmt.Fprint(out, "  - Service records now match the backup: services created later are not listed, and removed ones show as missing. Their containers and data were not touched.\n")
	fmt.Fprint(out, "  - Start GoForge: docker compose start goforge\n")
	if res.PreviousDB != "" {
		fmt.Fprintf(out, "  - Undo (with GoForge stopped): ALTER DATABASE %s RENAME TO %s_undone; ALTER DATABASE %s RENAME TO %s;\n",
			plan.Database, plan.Database, res.PreviousDB, plan.Database)
	}
}
