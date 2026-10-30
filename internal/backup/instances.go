package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// Templates with a backup target in v1. Everything else (redis, the shared
// redis included) is reported as skipped, never silently dropped.
const (
	templatePostgres    = "postgres"
	templateGitea       = "gitea"
	templateVaultwarden = "vaultwarden"
)

// ContainerExecer is the slice of the Docker client the instance targets use.
type ContainerExecer interface {
	ExecStream(ctx context.Context, id string, opts docker.ExecOptions, stdout io.Writer) (docker.ExecStreamResult, error)
	// Running reports whether the container exists and is running.
	Running(ctx context.Context, id string) (bool, error)
}

// DockerExecer adapts *docker.Client to ContainerExecer.
type DockerExecer struct{ Client *docker.Client }

// ExecStream implements ContainerExecer.
func (d DockerExecer) ExecStream(ctx context.Context, id string, opts docker.ExecOptions, w io.Writer) (docker.ExecStreamResult, error) {
	return d.Client.ExecStream(ctx, id, opts, w)
}

// Running implements ContainerExecer.
func (d DockerExecer) Running(ctx context.Context, id string) (bool, error) {
	info, err := d.Client.InspectContainer(ctx, id)
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.State != nil && info.State.Running, nil
}

// InstanceLister lists every service instance, across all users.
type InstanceLister interface {
	ListAll(ctx context.Context) ([]*models.ServiceInstance, error)
}

// InstanceSource turns service_instances rows into targets. It enumerates
// rows only; it never globs Docker volumes, so a volume GoForge does not
// manage is never read.
type InstanceSource struct {
	Instances InstanceLister
	Docker    ContainerExecer
	Redactor  *Redactor
	// Container maps an instance ID to its container name. Nil uses
	// ContainerName; tests point it at throwaway containers.
	Container func(instanceID string) string
}

// ContainerName is the Docker name GoForge gives an instance's container.
func ContainerName(instanceID string) string { return "goforge-service-" + instanceID }

// Targets implements Source.
func (s *InstanceSource) Targets(ctx context.Context) ([]Target, []Skipped, error) {
	list, err := s.Instances.ListAll(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list service instances: %w", err)
	}
	targets := make([]Target, 0, len(list))
	var skipped []Skipped
	for _, in := range list {
		skip := func(reason string) {
			skipped = append(skipped, Skipped{InstanceID: in.ID, Name: in.Name, Template: in.TemplateName, Reason: reason})
		}
		name := ContainerName
		if s.Container != nil {
			name = s.Container
		}
		base := instanceTarget{inst: in, container: name(in.ID), docker: s.Docker, redactor: s.Redactor}
		var t Target
		switch in.TemplateName {
		case templatePostgres:
			t = &PostgresTarget{base}
		case templateGitea:
			t = &GiteaTarget{base}
		case templateVaultwarden:
			t = &VaultwardenTarget{base}
		default:
			skip("template has no backup target in v1")
			continue
		}
		running, rerr := s.Docker.Running(ctx, base.container)
		if rerr != nil {
			return nil, nil, fmt.Errorf("inspect %s: %w", base.container, rerr)
		}
		if !running {
			skip("container is not running")
			continue
		}
		targets = append(targets, t)
	}
	return targets, skipped, nil
}

type instanceTarget struct {
	inst      *models.ServiceInstance
	container string
	docker    ContainerExecer
	redactor  *Redactor
}

func (b instanceTarget) Name() string {
	return b.inst.TemplateName + ":" + b.inst.Name + " (" + b.inst.ID + ")"
}

func (b instanceTarget) path(name string) string {
	return "services/" + safeSegment(b.inst.ID) + "/" + name
}

func (b instanceTarget) artifact(name string, kind Kind, db string, opts docker.ExecOptions) Artifact {
	ct := "application/octet-stream"
	switch kind {
	case KindPgRoles:
		ct = "application/sql"
	case KindTarGz:
		ct = "application/gzip"
	case KindPgDump, KindSQLite:
	}
	return Artifact{
		Path:        b.path(name),
		Kind:        kind,
		ContentType: ct,
		InstanceID:  b.inst.ID,
		Template:    b.inst.TemplateName,
		Database:    db,
		Dump: func(ctx context.Context, w io.Writer) error {
			return b.exec(ctx, opts, w)
		},
	}
}

// exec streams a command's stdout into w and fails on a non-zero exit code
// (other than okCodes), with redacted stderr in the error.
func (b instanceTarget) exec(ctx context.Context, opts docker.ExecOptions, w io.Writer, okCodes ...int) error {
	res, err := b.docker.ExecStream(ctx, b.container, opts, w)
	if err != nil {
		return err
	}
	if res.ExitCode == 0 {
		return nil
	}
	for _, c := range okCodes {
		if res.ExitCode == c {
			return nil
		}
	}
	return fmt.Errorf("exit code %d: %s", res.ExitCode, b.redactor.Redact(res.Stderr))
}

func sh(script string, args ...string) docker.ExecOptions {
	return docker.ExecOptions{Cmd: append([]string{"sh", "-c", script, "sh"}, args...)}
}

// PostgresTarget backs up a GoForge postgres instance: one pg_dump -Fc per
// non-template database, plus pg_dumpall --roles-only. Commands run inside
// the container as the in-container POSTGRES_USER over the local socket, so
// the encrypted admin password is never decrypted.
type PostgresTarget struct{ instanceTarget }

const pgUser = `"${POSTGRES_USER:-postgres}"`

// Plan implements Target.
func (t *PostgresTarget) Plan(ctx context.Context) ([]Artifact, error) {
	var out bytes.Buffer
	list := sh(`exec psql -X -q -tA -v ON_ERROR_STOP=1 -U ` + pgUser + ` -d "${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" -c "SELECT datname FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY datname"`)
	if err := t.exec(ctx, list, &limitedWriter{w: &out, n: 1 << 20}); err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	var arts []Artifact //nolint:prealloc // the line count includes blanks
	seen := map[string]bool{}
	for _, db := range strings.Split(out.String(), "\n") {
		db = strings.TrimSpace(db)
		if db == "" {
			continue
		}
		name := safeSegment(db)
		for seen[name] {
			name += "_"
		}
		seen[name] = true
		arts = append(arts, t.artifact(name+".dump", KindPgDump, db,
			sh(`exec pg_dump -U `+pgUser+` --format=custom -d "$1"`, db)))
	}
	if len(arts) == 0 {
		return nil, errors.New("no databases found")
	}
	arts = append(arts, t.artifact("roles.sql", KindPgRoles, "",
		sh(`exec pg_dumpall -U `+pgUser+` --roles-only`)))
	return arts, nil
}

// GiteaTarget backs up a Gitea instance's /data volume.
//
// Consistency: the SQLite database is copied with the sqlite3 online backup
// API (".backup") into a temp file, which takes a consistent snapshot while
// Gitea keeps running. The rest of /data (repositories, config, avatars,
// LFS) is tarred live, minus the database files and rebuildable indexers and
// queues. A push that lands mid-tar can leave that one repository needing
// `git fsck`; at 02:00 this is unlikely (hunch, not measured). Nothing is
// stopped and nothing in the volume is written except the temp file in /tmp.
type GiteaTarget struct{ instanceTarget }

// giteaSnapshot runs as the git user so it cannot change file ownership in
// the volume.
const giteaSnapshot = `set -eu
db="${GITEA__database__PATH:-}"
if [ -z "$db" ]; then
  ini="${GITEA_APP_INI:-/data/gitea/conf/app.ini}"
  if [ -r "$ini" ]; then
    db=$(awk -F= '/^[[:space:]]*\[/{s=$0; gsub(/[[:space:]]/,"",s)} s=="[database]" && $1 ~ /^[[:space:]]*PATH[[:space:]]*$/ {v=$2; gsub(/^[[:space:]]+|[[:space:]]+$/,"",v); print v; exit}' "$ini")
  fi
fi
db="${db:-/data/gitea/gitea.db}"
[ -f "$db" ] || { echo "gitea sqlite database not found at $db" >&2; exit 3; }
t=$(mktemp /tmp/goforge-backup.XXXXXX)
trap 'rm -f "$t"' EXIT
sqlite3 -bail -cmd ".timeout 30000" "$db" ".backup '$t'"
cat "$t"`

// giteaTar runs as root (the ssh host keys under /data are root-only) and
// only reads.
const giteaTar = `exec tar -C /data -czf - \
  --exclude=./gitea/gitea.db --exclude=./gitea/gitea.db-journal \
  --exclude=./gitea/gitea.db-wal --exclude=./gitea/gitea.db-shm \
  --exclude=./gitea/indexers --exclude=./gitea/queues .`

// Plan implements Target.
func (t *GiteaTarget) Plan(context.Context) ([]Artifact, error) {
	snap := sh(giteaSnapshot)
	snap.User = "git"
	return []Artifact{
		t.artifact("gitea.sqlite3", KindSQLite, "gitea", snap),
		t.artifact("data.tar.gz", KindTarGz, "", sh(giteaTar)),
	}, nil
}

// VaultwardenTarget backs up a Vaultwarden instance's /data volume.
//
// Consistency: `/vaultwarden backup` (VACUUM INTO) writes a consistent copy
// of db.sqlite3 to /data/db_<timestamp>.sqlite3 while the server keeps
// running. The copy is streamed and then removed; only that exact file is
// deleted. The rest of /data (rsa_key*, attachments, sends, config.json) is
// tarred live minus the database, icon cache, and tmp.
type VaultwardenTarget struct{ instanceTarget }

const vaultwardenSnapshot = `set -eu
out=$(/vaultwarden backup 2>&1) || { printf '%s\n' "$out" >&2; exit 3; }
p=$(printf '%s\n' "$out" | sed -n "s/.*Backup to '\(.*\)' was successful.*/\1/p" | tail -n 1)
# With the default DATA_FOLDER the path is relative to the working dir
# ("data/db_....sqlite3").
case "$p" in /*) ;; ?*) p="${PWD%/}/$p" ;; esac
case "$p" in
  /data/db_*.sqlite3) ;;
  *) echo "unexpected vaultwarden backup path" >&2; exit 4 ;;
esac
case "${p#/data/}" in */*) echo "unexpected vaultwarden backup path" >&2; exit 4 ;; esac
trap 'rm -f -- "$p"' EXIT
cat -- "$p"`

// vaultwardenTar accepts GNU tar exit code 1 ("file changed as we read
// it"): the archive is still valid, the changed file may be torn.
const vaultwardenTar = `rc=0
tar -C /data --warning=no-file-changed -czf - \
  --exclude=./db.sqlite3 --exclude=./db.sqlite3-wal --exclude=./db.sqlite3-shm \
  --exclude='./db_*.sqlite3' --exclude=./icon_cache --exclude=./tmp . || rc=$?
[ "$rc" -le 1 ] || exit "$rc"`

// Plan implements Target.
func (t *VaultwardenTarget) Plan(context.Context) ([]Artifact, error) {
	return []Artifact{
		t.artifact("vaultwarden.sqlite3", KindSQLite, "vaultwarden", sh(vaultwardenSnapshot)),
		t.artifact("data.tar.gz", KindTarGz, "", sh(vaultwardenTar)),
	}, nil
}
