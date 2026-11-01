package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
)

var (
	serviceRestoreYes     bool
	serviceRestoreConfirm string
)

var backupRestoreServiceCmd = &cobra.Command{
	Use:   "restore-service <run-key> <instance-id>",
	Short: "Restore one Gitea or Vaultwarden service's data from a backup run (dry run unless --yes)",
	Long: `Restore the /data volume of one Gitea or Vaultwarden service instance from
one backup run (services/<instance-id>/ in that run). The GoForge database,
other services, and Postgres services are not touched.

Without --yes this is a dry run: it reads the manifest, inspects the
service container, checks the backup lock, prints what would happen, and
changes nothing.

With --yes it takes the backup lock, downloads the SQLite snapshot and
data.tar.gz and checks their sizes and SHA-256 against the manifest (any
mismatch stops here, with the service still running), then stops the
service, moves the current volume contents to
/data/.goforge-pre-restore-<UTC time>/ (kept, never deleted), extracts the
backup with its file owners, puts the SQLite snapshot at the database path
with its directory's owner, starts the service, and waits for its health
check. If extracting fails, the previous contents are put back and the
service is started again.

Confirm by typing the run key at the prompt, or pass --confirm=<run-key>.
Run it inside the running GoForge container (it needs the Docker socket):

  docker compose exec goforge ./goforge backup restore-service <run-key> <instance-id>
  docker compose exec goforge ./goforge backup restore-service <run-key> <instance-id> --yes`,
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runBackupRestoreService(cmd.Context(), args[0], args[1], cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

func init() {
	backupCmd.AddCommand(backupRestoreServiceCmd)
	backupRestoreServiceCmd.Flags().BoolVar(&serviceRestoreYes, "yes", false, "restore for real (without it, a dry run)")
	backupRestoreServiceCmd.Flags().StringVar(&serviceRestoreConfirm, "confirm", "", "the run key again, to confirm without a terminal prompt")
}

// serviceRestoreOpts are the command's flags plus where the bucket is, for
// the plan printout.
type serviceRestoreOpts struct {
	Yes     bool
	Confirm string
	Where   string
	Red     *backup.Redactor
}

func runBackupRestoreService(ctx context.Context, runKey, instanceID string, in io.Reader, out io.Writer) error {
	if err := checkServiceRestoreArgs(runKey, instanceID, serviceRestoreYes, serviceRestoreConfirm); err != nil {
		return err
	}
	t, err := loadBackupTarget(ctx, out)
	if err != nil {
		return err
	}
	dc, err := docker.NewClient(t.cfg.DockerHost)
	if err != nil {
		return errors.New(t.redactor.Redact(err.Error()))
	}
	defer dc.Close()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	r := &backup.ServiceRestorer{
		Store: t.store, Prefix: t.bc.Prefix, Docker: backup.DockerServiceOps{Client: dc},
		Redactor: t.redactor, Progress: out,
	}
	if db, derr := database.New(t.cfg); derr == nil { //nolint:contextcheck // database.New takes no context
		defer db.Close()
		r.Lock = repositories.NewBackupRunRepository(db.DB).Lock
	} else {
		fmt.Fprint(out, "note: the GoForge database is not reachable; restoring without the backup lock (make sure no backup is running)\n")
	}
	return restoreService(ctx, r, runKey, instanceID, serviceRestoreOpts{
		Yes: serviceRestoreYes, Confirm: serviceRestoreConfirm,
		Where: fmt.Sprintf("s3://%s/%s%s/", t.bc.Bucket, t.bc.Prefix, runKey), Red: t.redactor,
	}, in, out)
}

func checkServiceRestoreArgs(runKey, instanceID string, yes bool, confirm string) error {
	if !backup.ValidRunKey(runKey) {
		return fmt.Errorf("%w: %q (want YYYY-MM-DD/YYYYMMDDTHHMMSSZ, as printed by goforge backup list)", backup.ErrBadRunKey, runKey)
	}
	if !backup.ValidInstanceID(instanceID) {
		return fmt.Errorf("%w: %q", backup.ErrBadInstanceID, instanceID)
	}
	if confirm != "" && !yes {
		return errors.New("--confirm only takes effect with --yes")
	}
	return nil
}

// restoreService plans, prints, confirms, and runs one service restore.
func restoreService(ctx context.Context, r *backup.ServiceRestorer, runKey, instanceID string, o serviceRestoreOpts, in io.Reader, out io.Writer) error {
	red := o.Red
	if red == nil {
		red = backup.NewRedactor()
	}
	plan, err := r.Plan(ctx, runKey, instanceID)
	if err != nil {
		return errors.New(red.Redact(err.Error()))
	}
	info, preErr := r.Preflight(ctx, plan)
	printServiceRestorePlan(out, plan, info, o.Where, preErr, red)

	if !o.Yes {
		fmt.Fprintf(out, "\nDry run: nothing changed. To restore, run:\n  goforge backup restore-service %s %s --yes\n", runKey, instanceID)
		if preErr != nil {
			return errors.New("restore would be refused: " + red.Redact(preErr.Error()))
		}
		return nil
	}
	if preErr != nil {
		return errors.New(red.Redact(preErr.Error()))
	}
	if cerr := confirmTyped(runKey, o.Confirm, fmt.Sprintf("This stops %s and replaces its /data.", plan.Container), in, out); cerr != nil {
		return cerr
	}
	fmt.Fprint(out, "\n")
	res, err := r.Restore(ctx, plan)
	if err != nil {
		return errors.New(red.Redact(err.Error()))
	}
	printServiceRestoreResult(out, plan, res)
	return nil
}

func printServiceRestorePlan(out io.Writer, plan *backup.ServiceRestorePlan, info *backup.ServiceContainerInfo, where string, preErr error, red *backup.Redactor) {
	m := plan.Manifest
	started := m.StartedAt.In(time.Local).Format("2006-01-02 15:04:05 MST")
	fmt.Fprint(out, "Service restore plan\n")
	if where != "" {
		fmt.Fprintf(out, "  run:        %s (%s)\n", plan.RunKey, where)
	} else {
		fmt.Fprintf(out, "  run:        %s\n", plan.RunKey)
	}
	fmt.Fprintf(out, "  taken:      %s, trigger %s, GoForge %s\n", started, m.Trigger, m.GoForgeVersion)
	fmt.Fprintf(out, "  instance:   %s (%s), container %s\n", plan.InstanceID, plan.Template, plan.Container)
	if info != nil {
		state := "stopped"
		if info.Running {
			state = "running; it is stopped for the restore and started after"
		}
		fmt.Fprintf(out, "  container:  %s\n", state)
	}
	fmt.Fprintf(out, "  database:   %s, %d bytes, sha256 %s\n", plan.Database.Key, plan.Database.Bytes, plan.Database.SHA256)
	fmt.Fprintf(out, "  data:       %s, %d bytes, sha256 %s\n", plan.Data.Key, plan.Data.Bytes, plan.Data.SHA256)
	fmt.Fprint(out, "  replaces:   everything in the service's /data volume; the current contents are moved to /data/.goforge-pre-restore-<UTC time>/ (kept)\n")
	if preErr != nil {
		fmt.Fprintf(out, "  preflight:  REFUSED: %s\n", red.Redact(preErr.Error()))
	} else {
		fmt.Fprint(out, "  preflight:  ok (container found, backup lock free)\n")
	}
}

func printServiceRestoreResult(out io.Writer, plan *backup.ServiceRestorePlan, res *backup.ServiceRestoreResult) {
	started := plan.Manifest.StartedAt.In(time.Local).Format("2006-01-02 15:04:05 MST")
	fmt.Fprintf(out, "\nRestored %s (%s) from %s (taken %s); it is running and healthy.\n", plan.InstanceID, plan.Template, plan.RunKey, started)
	fmt.Fprintf(out, "The previous data is kept in %s inside the volume. It is never deleted automatically\n", res.AsideDir)
	fmt.Fprint(out, "and is left out of nightly backups. Delete it once the restored service checks out.\n")
	fmt.Fprint(out, "\nNext:\n")
	switch plan.Template {
	case "gitea":
		fmt.Fprint(out, "  - Sign in and check repositories, users, and recent pushes. Pushes made after the backup are gone.\n")
	case "vaultwarden":
		fmt.Fprint(out, "  - Sign in and check the vault. Items saved after the backup are gone; clients sync to the restored state.\n")
	}
	fmt.Fprintf(out, "  - Undo: run restore-service again with a newer run key, or (service stopped) move the contents of %s back to /data.\n", res.AsideDir)
}
