package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
)

// Service restore refusals. Each one is returned before the service
// container is stopped.
var (
	// ErrServiceNotInRun means the run's manifest has no artifacts for the
	// instance (it did not exist, was stopped, or was skipped that night).
	ErrServiceNotInRun = errors.New("the run has no artifacts for this service instance")
	// ErrServiceRestoreUnsupported means the instance's template has no
	// scripted restore (only postgres, gitea, and vaultwarden have one).
	ErrServiceRestoreUnsupported = errors.New("restore-service supports postgres, gitea, and vaultwarden instances only")
	// ErrServiceContainerMissing means the instance's container does not
	// exist. Redeploy the service from GoForge first, then restore into it.
	ErrServiceContainerMissing = errors.New("the service container does not exist; deploy the service from GoForge first, then restore into it")
	// ErrBadInstanceID is an instance ID that cannot be a GoForge instance.
	ErrBadInstanceID = errors.New("instance ID must be the service's ID as shown on its page (letters, digits, '-', '_', '.')")
)

// serviceMount is the data volume mount of the Gitea and Vaultwarden
// templates; postgresMount is the Postgres template's (PGDATA).
const (
	serviceMount  = "/data"
	postgresMount = "/var/lib/postgresql/data"
)

// serviceMounts is every template restore-service handles, with the path
// of its data volume in the container (templates/<name>.yaml).
var serviceMounts = map[string]string{
	templateGitea:       serviceMount,
	templateVaultwarden: serviceMount,
	templatePostgres:    postgresMount,
}

// ServiceMount is the data volume path restore-service replaces for the
// template, or "" when it does not handle the template.
func ServiceMount(template string) string { return serviceMounts[template] }

// Names of the copies restore-service keeps inside the volume. Nightly
// backups leave them out (restoreAsideExcludes).
const (
	preRestorePrefix    = ".goforge-pre-restore-"
	failedRestorePrefix = ".goforge-failed-restore-"
)

// restoreNewPrefix names the cluster a Postgres restore builds inside the
// volume before it moves it into place. A failed restore leaves it in the
// failed-restore folder.
const restoreNewPrefix = ".goforge-restore-new-"

// restoreAsideExcludes keeps the kept copies out of the nightly data tar.
const restoreAsideExcludes = `--exclude='./` + preRestorePrefix + `*' --exclude='./` + failedRestorePrefix + `*'`

// restoreStage is where the verified artifacts are copied in the helper
// container (its own filesystem, not the volume).
const restoreStage = "/tmp/goforge-restore"

// ServiceContainerInfo is what restore-service needs from docker inspect.
type ServiceContainerInfo struct {
	ID string
	// Image is the image ID the container runs; the helper uses it too.
	Image string
	// ImageRef is the configured image reference, for the plan only.
	ImageRef string
	Running  bool
	// GiteaDBPath is GITEA__database__PATH from the container env, or "".
	GiteaDBPath string
	// PostgresUser is POSTGRES_USER from the container env, or "". No other
	// environment value is read (never POSTGRES_PASSWORD).
	PostgresUser string
}

// ServiceDocker is the Docker surface restore-service uses.
type ServiceDocker interface {
	// Inspect returns ErrServiceContainerMissing when name does not exist.
	Inspect(ctx context.Context, name string) (*ServiceContainerInfo, error)
	Stop(ctx context.Context, id string) error
	Start(ctx context.Context, id string) error
	WaitHealthy(ctx context.Context, id string) error
	StartHelper(ctx context.Context, opts docker.HelperOptions) (string, error)
	RemoveHelper(ctx context.Context, id string) error
	CopyToContainer(ctx context.Context, id, dest string, tarStream io.Reader) error
	ExecStream(ctx context.Context, id string, opts docker.ExecOptions, stdout io.Writer) (docker.ExecStreamResult, error)
}

// DockerServiceOps adapts *docker.Client to ServiceDocker.
type DockerServiceOps struct {
	Client *docker.Client
	// Poll is the health poll interval (default 2s).
	Poll time.Duration
}

// Inspect implements ServiceDocker.
func (d DockerServiceOps) Inspect(ctx context.Context, name string) (*ServiceContainerInfo, error) {
	res, err := d.Client.InspectContainer(ctx, name)
	if cerrdefs.IsNotFound(err) {
		return nil, ErrServiceContainerMissing
	}
	if err != nil {
		return nil, err
	}
	info := &ServiceContainerInfo{ID: res.ID, Image: res.Image, Running: res.State != nil && res.State.Running}
	if res.Config != nil {
		info.ImageRef = res.Config.Image
		for _, kv := range res.Config.Env {
			if v, ok := strings.CutPrefix(kv, "GITEA__database__PATH="); ok {
				info.GiteaDBPath = v
			}
			if v, ok := strings.CutPrefix(kv, "POSTGRES_USER="); ok {
				info.PostgresUser = v
			}
		}
	}
	return info, nil
}

// Stop implements ServiceDocker.
func (d DockerServiceOps) Stop(ctx context.Context, id string) error {
	return d.Client.StopContainer(ctx, id)
}

// Start implements ServiceDocker.
func (d DockerServiceOps) Start(ctx context.Context, id string) error {
	return d.Client.StartContainer(ctx, id)
}

// WaitHealthy implements ServiceDocker.
func (d DockerServiceOps) WaitHealthy(ctx context.Context, id string) error {
	poll := d.Poll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	return d.Client.WaitHealthy(ctx, id, poll)
}

// StartHelper implements ServiceDocker.
func (d DockerServiceOps) StartHelper(ctx context.Context, opts docker.HelperOptions) (string, error) {
	return d.Client.StartHelper(ctx, opts)
}

// RemoveHelper implements ServiceDocker.
func (d DockerServiceOps) RemoveHelper(ctx context.Context, id string) error {
	return d.Client.RemoveHelper(ctx, id)
}

// CopyToContainer implements ServiceDocker.
func (d DockerServiceOps) CopyToContainer(ctx context.Context, id, dest string, r io.Reader) error {
	return d.Client.CopyToContainer(ctx, id, dest, r)
}

// ExecStream implements ServiceDocker.
func (d DockerServiceOps) ExecStream(ctx context.Context, id string, opts docker.ExecOptions, w io.Writer) (docker.ExecStreamResult, error) {
	return d.Client.ExecStream(ctx, id, opts, w)
}

// ServiceRestorer restores one Postgres, Gitea, or Vaultwarden instance's
// data volume from one backup run. It reads only <run>/manifest.json and
// that instance's services/<id>/ objects, and only touches that instance's
// container and a short-lived helper container that borrows its volume.
//
// The current volume contents are moved to <mount>/.goforge-pre-restore-<UTC
// time>/ (kept, never deleted) before the backup is restored.
type ServiceRestorer struct {
	Store  Store
	Prefix string
	Docker ServiceDocker
	// Container maps an instance ID to its container name. Nil uses
	// ContainerName; tests point it at throwaway containers.
	Container func(instanceID string) string
	// Lock takes the backup advisory lock for the whole restore, so a
	// nightly run cannot tar the volume halfway. Nil skips it (no GoForge
	// database reachable).
	Lock func(ctx context.Context) (unlock func(), err error)
	// TempDir holds the downloaded artifacts (mode 600) until Restore
	// returns.
	TempDir  string
	Redactor *Redactor
	// Progress gets one line per step. No secret is ever written to it.
	Progress io.Writer
	// HealthTimeout bounds the wait for the restarted service to report
	// healthy (default 5 minutes).
	HealthTimeout time.Duration
	Now           func() time.Time
}

// ServiceRestorePlan is what a service restore would do, from the manifest
// and docker inspect alone.
type ServiceRestorePlan struct {
	RunKey     string
	Manifest   *Manifest
	InstanceID string
	Template   string
	// Mount is the data volume's path in the container (/data, or PGDATA
	// for Postgres).
	Mount string
	// Gitea and Vaultwarden: Database is the SQLite snapshot artifact,
	// Data the /data tar.
	Database, Data ManifestArtifact
	// Postgres: Dumps is one pg_dump --format=custom archive per database
	// (ManifestArtifact.Database names it), in manifest order; Roles is
	// the pg_dumpall --roles-only script.
	Dumps     []ManifestArtifact
	Roles     ManifestArtifact
	Container string
}

// ServiceRestoreResult is a finished service restore.
type ServiceRestoreResult struct {
	Plan *ServiceRestorePlan
	// AsideDir is where the previous volume contents were moved, inside the
	// volume (for example /data/.goforge-pre-restore-20261101t130000z, or
	// /var/lib/postgresql/data/.goforge-pre-restore-... for Postgres).
	AsideDir string
	// Healthy is true when the restarted container reported healthy.
	Healthy bool
}

func (r *ServiceRestorer) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *ServiceRestorer) progress(format string, args ...any) {
	if r.Progress != nil {
		fmt.Fprintf(r.Progress, format+"\n", args...)
	}
}

func (r *ServiceRestorer) redact(s string) string {
	if r.Redactor == nil {
		return s
	}
	return r.Redactor.Redact(s)
}

func (r *ServiceRestorer) containerName(id string) string {
	if r.Container != nil {
		return r.Container(id)
	}
	return ContainerName(id)
}

// ValidInstanceID reports whether id can name an instance's backup folder
// as is (the backup writes services/<id>/).
func ValidInstanceID(id string) bool {
	return id != "" && len(id) <= 128 && safeSegment(id) == id
}

// serviceArtifactNames is the SQLite snapshot each SQLite template writes
// next to data.tar.gz.
var serviceArtifactNames = map[string]string{
	templateGitea:       "gitea.sqlite3",
	templateVaultwarden: "vaultwarden.sqlite3",
}

// Plan reads the run's manifest and finds the instance's artifacts. It
// does not touch Docker.
func (r *ServiceRestorer) Plan(ctx context.Context, runKey, instanceID string) (*ServiceRestorePlan, error) {
	if !ValidInstanceID(instanceID) {
		return nil, ErrBadInstanceID
	}
	m, err := ReadManifest(ctx, r.Store, r.Prefix, runKey)
	if err != nil {
		return nil, err
	}
	arts := m.InstanceArtifacts(instanceID)
	if len(arts) == 0 {
		for _, s := range m.Skipped {
			if s.InstanceID == instanceID {
				return nil, fmt.Errorf("%w: run %s skipped it (%s)", ErrServiceNotInRun, runKey, s.Reason)
			}
		}
		return nil, fmt.Errorf("%w: %s in run %s", ErrServiceNotInRun, instanceID, runKey)
	}
	tpl := arts[0].Template
	mount, ok := serviceMounts[tpl]
	if !ok {
		return nil, fmt.Errorf("%w (instance %s is %q)", ErrServiceRestoreUnsupported, instanceID, tpl)
	}
	plan := &ServiceRestorePlan{RunKey: runKey, Manifest: m, InstanceID: instanceID, Template: tpl, Mount: mount, Container: r.containerName(instanceID)}
	dir := "services/" + instanceID + "/"
	if tpl == templatePostgres {
		return planPostgres(plan, arts, dir)
	}
	dbName := serviceArtifactNames[tpl]
	var haveDB, haveData bool
	for _, a := range arts {
		switch a.Key {
		case dir + dbName:
			if a.Kind != KindSQLite {
				return nil, fmt.Errorf("%s has kind %q, want %q", a.Key, a.Kind, KindSQLite)
			}
			plan.Database, haveDB = a, true
		case dir + "data.tar.gz":
			if a.Kind != KindTarGz {
				return nil, fmt.Errorf("%s has kind %q, want %q", a.Key, a.Kind, KindTarGz)
			}
			plan.Data, haveData = a, true
		}
	}
	if !haveDB || !haveData {
		return nil, fmt.Errorf("run %s is missing %s%s or %sdata.tar.gz for this %s instance", runKey, dir, dbName, dir, tpl)
	}
	return plan, nil
}

// planPostgres finds a Postgres instance's roles.sql and one dump per
// database.
func planPostgres(plan *ServiceRestorePlan, arts []ManifestArtifact, dir string) (*ServiceRestorePlan, error) {
	var haveRoles bool
	seen := map[string]bool{}
	for _, a := range arts {
		switch {
		case a.Key == dir+"roles.sql":
			if a.Kind != KindPgRoles {
				return nil, fmt.Errorf("%s has kind %q, want %q", a.Key, a.Kind, KindPgRoles)
			}
			plan.Roles, haveRoles = a, true
		case strings.HasPrefix(a.Key, dir) && strings.HasSuffix(a.Key, ".dump"):
			if a.Kind != KindPgDump {
				return nil, fmt.Errorf("%s has kind %q, want %q", a.Key, a.Kind, KindPgDump)
			}
			if a.Database == "" {
				return nil, fmt.Errorf("%s does not name its database in the manifest", a.Key)
			}
			if seen[a.Database] {
				return nil, fmt.Errorf("the run has two dumps of database %q for this instance", a.Database)
			}
			seen[a.Database] = true
			plan.Dumps = append(plan.Dumps, a)
		}
	}
	if !haveRoles || len(plan.Dumps) == 0 {
		return nil, fmt.Errorf("run %s is missing %sroles.sql or any %s<database>.dump for this postgres instance", plan.RunKey, dir, dir)
	}
	return plan, nil
}

// stageFile is one verified artifact and its name in the helper's
// restoreStage directory.
type stageFile struct {
	name string
	art  ManifestArtifact
	path string // the downloaded copy, once fetched
}

// stageFiles lists what Restore downloads, verifies, and copies into the
// helper, in order.
func (p *ServiceRestorePlan) stageFiles() []stageFile {
	if p.Template == templatePostgres {
		out := make([]stageFile, 0, len(p.Dumps)+1)
		out = append(out, stageFile{name: "roles.sql", art: p.Roles})
		for i, d := range p.Dumps {
			out = append(out, stageFile{name: strconv.Itoa(i) + ".dump", art: d})
		}
		return out
	}
	return []stageFile{{name: "db.sqlite3", art: p.Database}, {name: "data.tar.gz", art: p.Data}}
}

// restoreCmd is the helper command that restores the staged files.
func (p *ServiceRestorePlan) restoreCmd(info *ServiceContainerInfo, stamp string) []string {
	if p.Template == templatePostgres {
		cmd := []string{"sh", "-c", restorePostgresScript, "sh", p.Mount, stamp, info.Superuser()}
		for _, d := range p.Dumps {
			cmd = append(cmd, d.Database)
		}
		return cmd
	}
	return []string{"sh", "-c", restoreServiceScript, "sh", p.Template, stamp, info.GiteaDBPath}
}

// defaultPostgresSuperuser is the official image's superuser when
// POSTGRES_USER is unset. The image names it after its maintenance database.
const defaultPostgresSuperuser = maintenanceDB

// Superuser is the role a Postgres restore builds the cluster with: the
// container's POSTGRES_USER, else the image default.
func (i *ServiceContainerInfo) Superuser() string {
	if i.PostgresUser != "" {
		return i.PostgresUser
	}
	return defaultPostgresSuperuser
}

// IsPostgres reports whether the plan restores a Postgres service.
func (p *ServiceRestorePlan) IsPostgres() bool { return p.Template == templatePostgres }

// Preflight checks the restore can start now: the container exists and the
// backup lock is free. It changes nothing.
func (r *ServiceRestorer) Preflight(ctx context.Context, plan *ServiceRestorePlan) (*ServiceContainerInfo, error) {
	info, err := r.inspect(ctx, plan)
	if err != nil {
		return nil, err
	}
	if r.Lock != nil {
		unlock, lerr := r.lock(ctx)
		if lerr != nil {
			return nil, lerr
		}
		unlock()
	}
	return info, nil
}

func (r *ServiceRestorer) inspect(ctx context.Context, plan *ServiceRestorePlan) (*ServiceContainerInfo, error) {
	info, err := r.Docker.Inspect(ctx, plan.Container)
	if err != nil {
		if errors.Is(err, ErrServiceContainerMissing) {
			return nil, fmt.Errorf("%w (%s)", ErrServiceContainerMissing, plan.Container)
		}
		return nil, fmt.Errorf("inspect %s: %s", plan.Container, r.redact(err.Error()))
	}
	if info.Image == "" {
		return nil, fmt.Errorf("inspect %s: no image ID", plan.Container)
	}
	if plan.Template == templatePostgres {
		if u := info.Superuser(); len(u) > 63 || strings.HasPrefix(u, "-") || strings.ContainsAny(u, "\x00\n\r") {
			return nil, fmt.Errorf("inspect %s: POSTGRES_USER is not a usable role name", plan.Container)
		}
	}
	return info, nil
}

func (r *ServiceRestorer) lock(ctx context.Context) (func(), error) {
	unlock, err := r.Lock(ctx)
	if errors.Is(err, repositories.ErrBackupLocked) {
		return nil, ErrRestoreBackupRunning
	}
	if err != nil {
		return nil, fmt.Errorf("backup lock: %s", r.redact(err.Error()))
	}
	return unlock, nil
}

// Restore runs the plan:
//
//  1. take the backup lock, download every artifact and check its size
//     and SHA-256 (any refusal here leaves the service running, untouched);
//  2. stop the service container;
//  3. start a helper container from the service's own image with its
//     volume (--volumes-from), no network, as root; copy the artifacts in;
//  4. move the current volume contents to <mount>/.goforge-pre-restore-<UTC
//     time>/, then
//     - Gitea, Vaultwarden: extract data.tar.gz (ownership from the tar),
//     copy the SQLite snapshot to the database path and give it its
//     directory's owner;
//     - Postgres: initdb a new cluster (as the data directory's owner, the
//     superuser named POSTGRES_USER) with the previous configuration
//     files, start it on a private socket only, load roles.sql, pg_restore
//     --create each database, stop it, and move it into PGDATA;
//  5. remove the helper, start the service, and wait for its health check.
//
// When step 4 fails, the partial restore is moved to
// <mount>/.goforge-failed-restore-<UTC time>/, the previous contents are
// put back, and the service is started again. Nothing in the volume is
// deleted.
func (r *ServiceRestorer) Restore(ctx context.Context, plan *ServiceRestorePlan) (*ServiceRestoreResult, error) {
	if r.Lock != nil {
		unlock, err := r.lock(ctx)
		if err != nil {
			return nil, err
		}
		defer unlock()
		r.progress("backup lock taken; no backup run can start until the restore ends")
	}
	info, err := r.inspect(ctx, plan)
	if err != nil {
		return nil, err
	}

	files := plan.stageFiles()
	defer func() {
		for i := range files {
			if files[i].path != "" {
				_ = os.Remove(files[i].path)
			}
		}
	}()
	for i := range files {
		path, ferr := r.fetch(ctx, plan, files[i].art)
		if ferr != nil {
			return nil, ferr
		}
		files[i].path = path
	}

	stamp := strings.ToLower(r.now().UTC().Format(runIDLayout))
	res := &ServiceRestoreResult{Plan: plan}

	if info.Running {
		if err := r.Docker.Stop(ctx, info.ID); err != nil {
			return nil, fmt.Errorf("stop %s: %s; nothing changed", plan.Container, r.redact(err.Error()))
		}
		r.progress("stopped %s", plan.Container)
	} else {
		r.progress("%s is not running; it will be started after the restore", plan.Container)
	}
	// From here on the service must be started again whatever happens.
	restoreErr := r.restoreVolume(ctx, plan, info, stamp, files)
	if restoreErr == nil {
		res.AsideDir = plan.Mount + "/" + preRestorePrefix + stamp
	}

	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.healthTimeout())
	defer cancel()
	if err := r.Docker.Start(sctx, info.ID); err != nil {
		startErr := fmt.Errorf("start %s: %s; start it from the GoForge service page", plan.Container, r.redact(err.Error()))
		return nil, errors.Join(restoreErr, startErr)
	}
	r.progress("started %s; waiting for its health check", plan.Container)
	if restoreErr != nil {
		return nil, restoreErr
	}
	if err := r.Docker.WaitHealthy(sctx, info.ID); err != nil {
		return res, fmt.Errorf("the data was restored and %s was started, but it is not healthy: %s; the previous data is kept in %s", plan.Container, r.redact(err.Error()), res.AsideDir)
	}
	res.Healthy = true
	r.progress("%s is healthy", plan.Container)
	return res, nil
}

func (r *ServiceRestorer) healthTimeout() time.Duration {
	if r.HealthTimeout > 0 {
		return r.HealthTimeout
	}
	return 5 * time.Minute
}

func (r *ServiceRestorer) fetch(ctx context.Context, plan *ServiceRestorePlan, a ManifestArtifact) (string, error) {
	path, n, err := fetchVerified(ctx, r.Store, ObjectKey(r.Prefix, plan.RunKey, a.Key), a, r.TempDir, r.redactor())
	if err != nil {
		return "", err
	}
	r.progress("downloaded %s: %d bytes, SHA-256 matches the manifest", a.Key, n)
	return path, nil
}

func (r *ServiceRestorer) redactor() *Redactor {
	if r.Redactor == nil {
		return NewRedactor()
	}
	return r.Redactor
}

// restoreVolume runs steps 3 and 4 in a helper container. The helper is
// always removed (never its volumes).
func (r *ServiceRestorer) restoreVolume(ctx context.Context, plan *ServiceRestorePlan, info *ServiceContainerInfo, stamp string, files []stageFile) error {
	helperName := "goforge-restore-" + plan.InstanceID + "-" + stamp
	hid, err := r.Docker.StartHelper(ctx, docker.HelperOptions{
		Name: helperName, Image: info.Image, VolumesFrom: info.ID,
		Entrypoint: []string{"sleep"}, Cmd: []string{"86400"},
		Labels: map[string]string{"goforge.restore-helper": plan.InstanceID},
	})
	if hid != "" {
		defer func() {
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if rerr := r.Docker.RemoveHelper(rctx, hid); rerr != nil {
				r.progress("could not remove helper container %s: %s; remove it by hand (docker rm -f %s, without -v)", helperName, r.redact(rerr.Error()), helperName)
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("helper container: %s; the volume is unchanged", r.redact(err.Error()))
	}
	r.progress("started helper container %s with the volumes of %s", helperName, plan.Container)

	if err := r.copyStage(ctx, hid, files); err != nil {
		return fmt.Errorf("copy the backup into the helper: %s; the volume is unchanged", r.redact(err.Error()))
	}
	r.progress("copied the verified artifacts into the helper")

	script := docker.ExecOptions{User: "0:0", Cmd: plan.restoreCmd(info, stamp)}
	if err := r.execLines(ctx, hid, script); err != nil {
		r.progress("restore failed: %s", r.redact(err.Error()))
		rb := docker.ExecOptions{User: "0:0", Cmd: []string{"sh", "-c", rollbackServiceScript, "sh", stamp, plan.Mount}}
		if rerr := r.execLines(context.WithoutCancel(ctx), hid, rb); rerr != nil {
			return fmt.Errorf("restore failed (%s) and putting the previous data back failed (%s): the previous data is in %s/%s%s, the partial restore in %s/%s%s",
				r.redact(err.Error()), r.redact(rerr.Error()), plan.Mount, preRestorePrefix, stamp, plan.Mount, failedRestorePrefix, stamp)
		}
		return fmt.Errorf("restore failed, the previous data was put back (the partial restore is kept in %s/%s%s): %s",
			plan.Mount, failedRestorePrefix, stamp, r.redact(err.Error()))
	}
	return nil
}

// copyStage streams a tar with the verified files into the helper's
// restoreStage directory, owned by root, mode 600.
func (r *ServiceRestorer) copyStage(ctx context.Context, hid string, files []stageFile) error {
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(writeStageTar(pw, files))
	}()
	err := r.Docker.CopyToContainer(ctx, hid, "/tmp", pr)
	pr.CloseWithError(errors.New("copy ended"))
	return err
}

func writeStageTar(w io.Writer, files []stageFile) error {
	tw := tar.NewWriter(w)
	base := strings.TrimPrefix(restoreStage, "/tmp/")
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: base + "/", Mode: 0o700, ModTime: time.Now()}); err != nil {
		return err
	}
	for i := range files {
		if err := addStageFile(tw, base+"/"+files[i].name, files[i].path); err != nil {
			return err
		}
	}
	return tw.Close()
}

func addStageFile(tw *tar.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if herr := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: st.Size(), ModTime: st.ModTime()}); herr != nil {
		return herr
	}
	_, err = io.Copy(tw, f)
	return err
}

// execLines runs a script in the helper, echoing its stdout lines to
// Progress, and fails on a non-zero exit with redacted stderr.
func (r *ServiceRestorer) execLines(ctx context.Context, hid string, opts docker.ExecOptions) error {
	var out strings.Builder
	res, err := r.Docker.ExecStream(ctx, hid, opts, &limitedWriter{w: &out, n: 64 << 10})
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line != "" {
			r.progress("  %s", r.redact(line))
		}
	}
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("exit code %d: %s", res.ExitCode, strings.TrimSpace(r.redact(res.Stderr)))
	}
	return nil
}

// restoreServiceScript runs as root in the helper. Arguments: template,
// UTC stamp, GITEA__database__PATH (may be empty). It never deletes: the
// current contents are moved aside first.
const restoreServiceScript = `set -eu
tpl="$1"; stamp="$2"; override="$3"
mnt=` + serviceMount + `; src=` + restoreStage + `
aside="$mnt/` + preRestorePrefix + `$stamp"
[ -d "$mnt" ] || { echo "$mnt is not mounted in the helper" >&2; exit 3; }
[ -s "$src/data.tar.gz" ] && [ -s "$src/db.sqlite3" ] || { echo "the staged artifacts are missing" >&2; exit 3; }
[ ! -e "$aside" ] || { echo "$aside already exists" >&2; exit 3; }
case "$tpl" in gitea|vaultwarden) ;; *) echo "unsupported template $tpl" >&2; exit 3 ;; esac
mkdir -m 700 "$aside"
find "$mnt" -mindepth 1 -maxdepth 1 ! -name '` + preRestorePrefix + `*' ! -name '` + failedRestorePrefix + `*' -exec mv -- {} "$aside"/ \;
echo "moved the current contents of $mnt to $aside (kept)"
tar -xzf "$src/data.tar.gz" -C "$mnt"
echo "extracted data.tar.gz into $mnt"
if [ "$tpl" = gitea ]; then
  db="$override"
  if [ -z "$db" ]; then
    ini="$mnt/gitea/conf/app.ini"
    if [ -r "$ini" ]; then
      db=$(` + giteaIniDBPath + `)
    fi
  fi
  db="${db:-$mnt/gitea/gitea.db}"
else
  db="$mnt/db.sqlite3"
fi
case "$db" in "$mnt"/*) ;; *) echo "database path $db is outside $mnt" >&2; exit 4 ;; esac
case "$db" in *..*) echo "database path $db contains .." >&2; exit 4 ;; esac
dir=$(dirname "$db")
mkdir -p "$dir"
cp -- "$src/db.sqlite3" "$db"
chown "$(stat -c '%u:%g' "$dir")" "$db"
old="$aside${db#"$mnt"}"
if [ -f "$old" ]; then chmod "$(stat -c '%a' "$old")" "$db"; else chmod 600 "$db"; fi
echo "restored the database to $db (owner $(stat -c '%u:%g' "$db"), mode $(stat -c '%a' "$db"))"`

// restorePostgresScript runs as root in the helper (the service's own
// postgres image). Arguments: the PGDATA mount, UTC stamp, superuser role
// (POSTGRES_USER), then one database name per staged <i>.dump. It never
// deletes: the current cluster is moved aside first, and a new cluster is
// built in <mount>/.goforge-restore-new-<stamp> (initdb refuses a
// directory that is not empty) and moved into the mount only once every
// database is loaded. The temporary server listens on a private socket
// only, with a trust-only hba file there; the previous postgresql.conf,
// postgresql.auto.conf, pg_hba.conf, and pg_ident.conf are carried over.
// Errors are trimmed and password hashes masked before they reach stderr.
const restorePostgresScript = `set -eu
mnt="$1"; stamp="$2"; su="$3"; shift 3
src=` + restoreStage + `
aside="$mnt/` + preRestorePrefix + `$stamp"; new="$mnt/` + restoreNewPrefix + `$stamp"
sock=/tmp/goforge-restore-sock; err=/tmp/goforge-restore-err
redact() { sed -E -e "s/SCRAM-SHA-256[^' ]*/SCRAM-SHA-256<redacted>/g" -e "s/md5[0-9a-f]{32}/md5<redacted>/g" -e "s/PASSWORD '[^']*'/PASSWORD '<redacted>'/g" "$1" | tail -n 20 >&2; }
[ -d "$mnt" ] || { echo "$mnt is not mounted in the helper" >&2; exit 3; }
[ "$#" -gt 0 ] || { echo "no databases to restore" >&2; exit 3; }
[ -s "$src/roles.sql" ] || { echo "the staged artifacts are missing" >&2; exit 3; }
i=0
for db in "$@"; do [ -s "$src/$i.dump" ] || { echo "the staged artifacts are missing" >&2; exit 3; }; i=$((i+1)); done
[ ! -e "$aside" ] && [ ! -e "$new" ] || { echo "$aside or $new already exists" >&2; exit 3; }
for b in initdb pg_ctl pg_restore psql gosu; do command -v "$b" >/dev/null 2>&1 || { echo "$b is not in the service image" >&2; exit 3; }; done
owner=$(stat -c '%u:%g' "$mnt")
case "$owner" in 0:*) owner="$(id -u postgres):$(id -g postgres)" ;; esac
aspg() { gosu "$owner" "$@"; }
mkdir -m 700 "$aside"; chown "$owner" "$aside"
find "$mnt" -mindepth 1 -maxdepth 1 ! -name '` + preRestorePrefix + `*' ! -name '` + failedRestorePrefix + `*' -exec mv -- {} "$aside"/ \;
echo "moved the current contents of $mnt to $aside (kept)"
mkdir -m 700 "$new"; chown "$owner" "$new"
chown -R "$owner" "$src"
aspg initdb -D "$new" -U "$su" --auth-local=trust --auth-host=scram-sha-256 >"$err" 2>&1 || { redact "$err"; exit 5; }
for f in postgresql.conf postgresql.auto.conf pg_hba.conf pg_ident.conf; do
  if [ -f "$aside/$f" ]; then cp -- "$aside/$f" "$new/$f"; fi
done
# The image's entrypoint adds this line on first start; a cluster restored
# into an empty volume needs it too.
[ -f "$aside/pg_hba.conf" ] || printf '\nhost all all all scram-sha-256\n' >> "$new/pg_hba.conf"
chown -R "$owner" "$new"
echo "initialized a new cluster for superuser $su with the previous configuration files"
mkdir -p -m 700 "$sock"; chown "$owner" "$sock"
printf 'local all all trust\n' > "$sock/hba.conf"; chown "$owner" "$sock/hba.conf"
trap 'aspg pg_ctl -D "$new" -m immediate -w stop >/dev/null 2>&1 || true' EXIT
aspg pg_ctl -D "$new" -w -t 300 -l "$sock/server.log" -o "-c listen_addresses='' -c unix_socket_directories=$sock -c hba_file=$sock/hba.conf" start >/dev/null 2>&1 || { redact "$sock/server.log"; exit 5; }
export PGHOST="$sock" PGUSER="$su"
# initdb already made the superuser; pg_dumpall writes CREATE ROLE for it
# too. Drop only that one line; ALTER ROLE still sets its password hash.
SU="$su" awk '$0 == "CREATE ROLE " ENVIRON["SU"] ";" || $0 == "CREATE ROLE \"" ENVIRON["SU"] "\";" { next } { print }' "$src/roles.sql" > "$src/roles.load.sql"
chown "$owner" "$src/roles.load.sql"; chmod 600 "$src/roles.load.sql"
aspg psql -X -q -v ON_ERROR_STOP=1 -v VERBOSITY=terse -d postgres -f "$src/roles.load.sql" >/dev/null 2>"$err" || { redact "$err"; exit 6; }
echo "loaded roles.sql"
i=0
for db in "$@"; do
  printf 'DROP DATABASE IF EXISTS :"db";\n' | aspg psql -X -q -v ON_ERROR_STOP=1 -v VERBOSITY=terse -v db="$db" -d template1 >/dev/null 2>"$err" || { redact "$err"; exit 7; }
  aspg pg_restore --create --exit-on-error -d template1 "$src/$i.dump" 2>"$err" || { redact "$err"; exit 7; }
  echo "restored database $db"
  i=$((i+1))
done
aspg pg_ctl -D "$new" -m fast -w -t 300 stop >/dev/null 2>&1 || { redact "$sock/server.log"; exit 8; }
trap - EXIT
find "$new" -mindepth 1 -maxdepth 1 -exec mv -- {} "$mnt"/ \;
rmdir "$new"
echo "moved the restored cluster into $mnt (owner $owner)"`

// rollbackServiceScript undoes a failed restore script: the partial
// restore goes to .goforge-failed-restore-<stamp>, the previous contents
// come back. Arguments: the UTC stamp, the volume mount.
const rollbackServiceScript = `set -eu
stamp="$1"; mnt="$2"
aside="$mnt/` + preRestorePrefix + `$stamp"; failed="$mnt/` + failedRestorePrefix + `$stamp"
[ -d "$aside" ] || { echo "nothing was moved aside; the volume is unchanged"; exit 0; }
mkdir -m 700 "$failed"
find "$mnt" -mindepth 1 -maxdepth 1 ! -name '` + preRestorePrefix + `*' ! -name '` + failedRestorePrefix + `*' -exec mv -- {} "$failed"/ \;
find "$aside" -mindepth 1 -maxdepth 1 -exec mv -- {} "$mnt"/ \;
rmdir "$aside"
echo "put the previous contents of $mnt back; the partial restore is kept in $failed"`

// InstanceArtifacts returns the manifest entries for one service instance.
func (m *Manifest) InstanceArtifacts(instanceID string) []ManifestArtifact {
	var out []ManifestArtifact
	for _, a := range m.Artifacts {
		if a.InstanceID == instanceID && instanceID != "" {
			out = append(out, a)
		}
	}
	return out
}
