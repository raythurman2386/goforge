package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	strategySQLiteVolume = "sqlite_volume"
	strategyVolume       = "volume"
)

// BackupSpec is one template's backup block. Strategy is postgres,
// sqlite_volume, or volume. gitea and vaultwarden are the older names for
// the two sqlite_volume builtins and still work.
type BackupSpec struct {
	Strategy  string
	Mount     string
	Snapshot  string
	Excludes  []string
	ChangedOK bool
}

// Usable reports whether a nightly run can dump this spec.
func (s BackupSpec) Usable() bool {
	cp := s
	return cp.Normalize() == nil
}

// Normalize fills builtin defaults and checks mount and excludes. It
// rejects an unknown strategy.
func (s *BackupSpec) Normalize() error {
	s.Strategy = strings.TrimSpace(s.Strategy)
	s.Mount = strings.TrimSpace(s.Mount)
	s.Snapshot = strings.TrimSpace(s.Snapshot)
	switch s.Strategy {
	case "":
		return errors.New("missing backup strategy")
	case templatePostgres:
		return nil
	case templateGitea:
		s.ChangedOK = false
		return s.applyBuiltin(templateGitea)
	case templateVaultwarden:
		s.ChangedOK = true
		return s.applyBuiltin(templateVaultwarden)
	case strategySQLiteVolume:
		if s.Snapshot == "" {
			return errors.New("sqlite_volume backup needs snapshot")
		}
		return s.applyBuiltin(s.Snapshot)
	case strategyVolume:
		if s.Mount == "" {
			return errors.New("volume backup needs mount")
		}
		if err := validateMount(s.Mount); err != nil {
			return err
		}
		return validateExcludes(s.Excludes)
	default:
		return fmt.Errorf("unknown backup strategy %q", s.Strategy)
	}
}

func (s *BackupSpec) applyBuiltin(name string) error {
	b, ok := snapshotBuiltins[name]
	if !ok {
		return fmt.Errorf("unknown snapshot %q", name)
	}
	if s.Mount == "" {
		s.Mount = b.mount
	}
	if err := validateMount(s.Mount); err != nil {
		return err
	}
	if s.Snapshot == "" {
		s.Snapshot = name
	}
	if s.Snapshot != name {
		return fmt.Errorf("snapshot %q does not match %s", s.Snapshot, name)
	}
	if len(s.Excludes) == 0 {
		s.Excludes = append([]string(nil), b.excludes...)
	}
	return validateExcludes(s.Excludes)
}

// BackupSpec implements StrategySource for a template name mapped to a
// strategy name. postgres, gitea, and vaultwarden expand to their builtins.
func (m StrategyMap) BackupSpec(name string) (BackupSpec, bool) {
	strategy, ok := m.BackupStrategy(name)
	if !ok {
		return BackupSpec{}, false
	}
	// Return the name even when it still needs a mount or snapshot. targetFor
	// reports that, instead of treating a known strategy as "no strategy".
	return BackupSpec{Strategy: strategy}, true
}

var (
	mountPattern   = regexp.MustCompile(`^/(?:[A-Za-z0-9_.-]+)(?:/[A-Za-z0-9_.-]+)*$`)
	excludePattern = regexp.MustCompile(`^\./[A-Za-z0-9_.*-]+(?:/[A-Za-z0-9_.*-]+)*$`)
)

func validateMount(mount string) error {
	if !mountPattern.MatchString(mount) || strings.Contains(mount, "..") {
		return fmt.Errorf("backup mount %q must be an absolute path of letters, digits, dots, dashes, and slashes", mount)
	}
	return nil
}

func validateExcludes(excludes []string) error {
	for _, ex := range excludes {
		if !excludePattern.MatchString(ex) || strings.Contains(ex, "..") {
			return fmt.Errorf("backup exclude %q must be a relative ./ path", ex)
		}
	}
	return nil
}

func (s BackupSpec) tarCommand() string {
	if s.sameTar(templateGitea) {
		return giteaTar
	}
	if s.sameTar(templateVaultwarden) {
		return vaultwardenTar
	}
	return buildTarScript(s.Mount, s.Excludes, s.ChangedOK)
}

func (s BackupSpec) sameTar(builtin string) bool {
	b, ok := snapshotBuiltins[builtin]
	if !ok || s.Mount != b.mount || s.ChangedOK != b.changedOK || len(s.Excludes) != len(b.excludes) {
		return false
	}
	for i := range b.excludes {
		if s.Excludes[i] != b.excludes[i] {
			return false
		}
	}
	return true
}

func buildTarScript(mount string, excludes []string, changedOK bool) string {
	var quoted []string
	for _, ex := range excludes {
		quoted = append(quoted, "--exclude="+quoteExclude(ex))
	}
	extra := strings.Join(quoted, " ")
	if extra != "" {
		extra += " "
	}
	if changedOK {
		return "rc=0\ntar -C " + mount + " --warning=no-file-changed -czf - " + extra + restoreAsideExcludes + " . || rc=$?\n" + `[ "$rc" -le 1 ] || exit "$rc"`
	}
	return "exec tar -C " + mount + " -czf - " + extra + restoreAsideExcludes + " ."
}

func quoteExclude(ex string) string {
	if strings.Contains(ex, "*") {
		return "'" + ex + "'"
	}
	return ex
}

type snapshotBuiltin struct {
	artifact  string
	user      string
	script    string
	mount     string
	excludes  []string
	changedOK bool
}

type sqliteVolumeTarget struct {
	instanceTarget
	spec BackupSpec
}

// Plan implements Target. The snapshot script is a named builtin. The tar
// uses the spec's mount and excludes, and the shipped gitea and vaultwarden
// lists keep the scripts those builtins already used.
func (t *sqliteVolumeTarget) Plan(context.Context) ([]Artifact, error) {
	return planSQLiteVolume(t.instanceTarget, t.spec)
}

func planSQLiteVolume(base instanceTarget, spec BackupSpec) ([]Artifact, error) {
	b, ok := snapshotBuiltins[spec.Snapshot]
	if !ok {
		return nil, fmt.Errorf("unknown snapshot %q", spec.Snapshot)
	}
	snap := sh(b.script, spec.Mount)
	snap.User = b.user
	tar := base
	tar.strategy = spec.Strategy
	tar.mount = spec.Mount
	tar.snapshot = spec.Snapshot
	return []Artifact{
		tar.artifact(b.artifact, KindSQLite, spec.Snapshot, snap),
		tar.artifact("data.tar.gz", KindTarGz, "", sh(spec.tarCommand())),
	}, nil
}

type volumeTarget struct {
	instanceTarget
	spec BackupSpec
}

// Plan implements Target. It tars the mount while the container runs.
// A file written during the tar can be torn. There is no snapshot command.
func (t *volumeTarget) Plan(context.Context) ([]Artifact, error) {
	base := t.instanceTarget
	base.strategy = t.spec.Strategy
	base.mount = t.spec.Mount
	return []Artifact{
		base.artifact("data.tar.gz", KindTarGz, "", sh(t.spec.tarCommand())),
	}, nil
}

var snapshotBuiltins = map[string]snapshotBuiltin{
	templateGitea: {
		artifact: "gitea.sqlite3",
		user:     "git",
		script:   giteaSnapshot,
		mount:    serviceMount,
		excludes: []string{
			"./gitea/gitea.db",
			"./gitea/gitea.db-journal",
			"./gitea/gitea.db-wal",
			"./gitea/gitea.db-shm",
			"./gitea/indexers",
			"./gitea/queues",
		},
	},
	templateVaultwarden: {
		artifact:  "vaultwarden.sqlite3",
		user:      "",
		script:    vaultwardenSnapshot,
		mount:     serviceMount,
		changedOK: true,
		excludes: []string{
			"./db.sqlite3",
			"./db.sqlite3-wal",
			"./db.sqlite3-shm",
			"./db_*.sqlite3",
			"./icon_cache",
			"./tmp",
		},
	},
}
