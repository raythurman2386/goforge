package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
)

const svcRunKey = "2026-10-30/20261030T070000Z"

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// putServiceRun writes a run with one gitea instance "g1", one vaultwarden
// instance "v1", one postgres instance "p1" (databases app and postgres),
// and one artifact of a template restore-service does not handle ("x1").
func putServiceRun(t *testing.T, s *memStore) map[string][]byte {
	t.Helper()
	bodies := map[string][]byte{
		"services/g1/gitea.sqlite3":       []byte("gitea-db"),
		"services/g1/data.tar.gz":         []byte("gitea-tar"),
		"services/v1/vaultwarden.sqlite3": []byte("vw-db"),
		"services/v1/data.tar.gz":         []byte("vw-tar"),
		"services/p1/app.dump":            []byte("pg"),
		"services/p1/postgres.dump":       []byte("pg-postgres"),
		"services/p1/roles.sql":           []byte("roles"),
		"services/x1/dump.rdb":            []byte("rdb"),
	}
	kinds := map[string]struct {
		kind Kind
		id   string
		tpl  string
		db   string
	}{
		"services/g1/gitea.sqlite3":       {KindSQLite, "g1", templateGitea, ""},
		"services/g1/data.tar.gz":         {KindTarGz, "g1", templateGitea, ""},
		"services/v1/vaultwarden.sqlite3": {KindSQLite, "v1", templateVaultwarden, ""},
		"services/v1/data.tar.gz":         {KindTarGz, "v1", templateVaultwarden, ""},
		"services/p1/app.dump":            {KindPgDump, "p1", templatePostgres, "app"},
		"services/p1/postgres.dump":       {KindPgDump, "p1", templatePostgres, maintenanceDB},
		"services/p1/roles.sql":           {KindPgRoles, "p1", templatePostgres, ""},
		"services/x1/dump.rdb":            {KindTarGz, "x1", "redis", ""},
	}
	m := &Manifest{Version: 1, RunKey: svcRunKey, RunID: "r", StartedAt: time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC),
		Skipped: []Skipped{{InstanceID: "off", Reason: "container not running"}}}
	for _, k := range []string{"services/g1/gitea.sqlite3", "services/g1/data.tar.gz", "services/v1/vaultwarden.sqlite3", "services/v1/data.tar.gz",
		"services/p1/app.dump", "services/p1/postgres.dump", "services/p1/roles.sql", "services/x1/dump.rdb"} {
		b := bodies[k]
		m.Artifacts = append(m.Artifacts, ManifestArtifact{Key: k, Kind: kinds[k].kind, InstanceID: kinds[k].id, Template: kinds[k].tpl, Database: kinds[k].db, Bytes: int64(len(b)), SHA256: sha(b)})
		mustPut(t, s, ObjectKey(testPrefix, svcRunKey, k), string(b))
	}
	body, _ := json.Marshal(m)
	mustPut(t, s, ObjectKey(testPrefix, svcRunKey, ManifestName), string(body))
	return bodies
}

type fakeSvcDocker struct {
	calls     []string
	info      *ServiceContainerInfo
	inspErr   error
	execFail  int // 1-based exec call that exits 1
	execN     int
	staged    map[string][]byte
	helper    docker.HelperOptions
	execCmds  [][]string
	unhealthy bool
}

func (f *fakeSvcDocker) Inspect(_ context.Context, name string) (*ServiceContainerInfo, error) {
	f.calls = append(f.calls, "inspect "+name)
	if f.inspErr != nil {
		return nil, f.inspErr
	}
	return f.info, nil
}
func (f *fakeSvcDocker) Stop(_ context.Context, id string) error {
	f.calls = append(f.calls, "stop "+id)
	return nil
}
func (f *fakeSvcDocker) Start(_ context.Context, id string) error {
	f.calls = append(f.calls, "start "+id)
	return nil
}
func (f *fakeSvcDocker) WaitHealthy(_ context.Context, id string) error {
	f.calls = append(f.calls, "healthy "+id)
	if f.unhealthy {
		return errors.New("unhealthy")
	}
	return nil
}
func (f *fakeSvcDocker) StartHelper(_ context.Context, o docker.HelperOptions) (string, error) {
	f.calls = append(f.calls, "helper")
	f.helper = o
	return "h1", nil
}
func (f *fakeSvcDocker) RemoveHelper(_ context.Context, id string) error {
	f.calls = append(f.calls, "rmhelper "+id)
	return nil
}
func (f *fakeSvcDocker) CopyToContainer(_ context.Context, id, dest string, r io.Reader) error {
	f.calls = append(f.calls, "copy "+id+" "+dest)
	f.staged = map[string][]byte{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(tr)
		if h.Typeflag == tar.TypeReg {
			if h.Mode != 0o600 {
				return errors.New("staged file not 0600")
			}
			f.staged[h.Name] = b
		}
	}
}
func (f *fakeSvcDocker) ExecStream(_ context.Context, id string, o docker.ExecOptions, w io.Writer) (docker.ExecStreamResult, error) {
	f.execN++
	f.calls = append(f.calls, "exec "+id)
	f.execCmds = append(f.execCmds, o.Cmd)
	if o.User != "0:0" {
		return docker.ExecStreamResult{ExitCode: 9, Stderr: "not root"}, nil
	}
	io.WriteString(w, "step done\n")
	if f.execN == f.execFail {
		return docker.ExecStreamResult{ExitCode: 1, Stderr: "tar: boom"}, nil
	}
	return docker.ExecStreamResult{}, nil
}

func newSvcRestorer(t *testing.T, s *memStore, d *fakeSvcDocker) (*ServiceRestorer, *bytes.Buffer) {
	var out bytes.Buffer
	return &ServiceRestorer{
		Store: s, Prefix: testPrefix, Docker: d, TempDir: t.TempDir(), Progress: &out,
		Container: func(id string) string { return "c-" + id },
		Now:       func() time.Time { return time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC) },
	}, &out
}

func TestServiceRestorePlan(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	putServiceRun(t, s)
	r, _ := newSvcRestorer(t, s, &fakeSvcDocker{})

	p, err := r.Plan(ctx, svcRunKey, "g1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Template != templateGitea || p.Database.Key != "services/g1/gitea.sqlite3" || p.Data.Key != "services/g1/data.tar.gz" || p.Container != "c-g1" {
		t.Fatalf("plan = %+v", p)
	}
	if p, err := r.Plan(ctx, svcRunKey, "v1"); err != nil || p.Database.Key != "services/v1/vaultwarden.sqlite3" {
		t.Fatalf("vw plan = %+v, %v", p, err)
	}
	if p, err := r.Plan(ctx, svcRunKey, "p1"); err != nil || p.Mount != "/var/lib/postgresql/data" || p.Roles.Key != "services/p1/roles.sql" ||
		len(p.Dumps) != 2 || p.Dumps[0].Database != "app" || p.Dumps[1].Database != maintenanceDB {
		t.Errorf("postgres plan = %+v, %v", p, err)
	}
	if p, _ := r.Plan(ctx, svcRunKey, "g1"); p.Mount != "/data" {
		t.Errorf("gitea mount = %q", p.Mount)
	}
	if _, err := r.Plan(ctx, svcRunKey, "x1"); !errors.Is(err, ErrServiceRestoreUnsupported) {
		t.Errorf("redis: %v", err)
	}
	if _, err := r.Plan(ctx, svcRunKey, "nope"); !errors.Is(err, ErrServiceNotInRun) {
		t.Errorf("missing: %v", err)
	}
	if _, err := r.Plan(ctx, svcRunKey, "off"); !errors.Is(err, ErrServiceNotInRun) || !strings.Contains(err.Error(), "container not running") {
		t.Errorf("skipped: %v", err)
	}
	for _, bad := range []string{"", "../g1", "g1/x", "a b"} {
		if _, err := r.Plan(ctx, svcRunKey, bad); !errors.Is(err, ErrBadInstanceID) {
			t.Errorf("Plan(%q) = %v", bad, err)
		}
	}
	if _, err := r.Plan(ctx, "bad", "g1"); !errors.Is(err, ErrBadRunKey) {
		t.Errorf("bad run key: %v", err)
	}
}

func TestServiceRestore_OrderAndStaging(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	bodies := putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "sha256:img", Running: true, GiteaDBPath: "/data/custom/g.db"}}
	r, out := newSvcRestorer(t, s, d)
	locked := 0
	r.Lock = func(context.Context) (func(), error) { locked++; return func() { locked-- }, nil }

	p, err := r.Plan(ctx, svcRunKey, "g1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Restore(ctx, p)
	if err != nil {
		t.Fatalf("Restore: %v\n%s", err, out)
	}
	want := "inspect c-g1,stop cid,helper,copy h1 /tmp,exec h1,rmhelper h1,start cid,healthy cid"
	if got := strings.Join(d.calls, ","); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	if locked != 0 {
		t.Error("lock not released")
	}
	if !res.Healthy || res.AsideDir != "/data/.goforge-pre-restore-20261101t130000z" {
		t.Errorf("result = %+v", res)
	}
	if string(d.staged["goforge-restore/db.sqlite3"]) != string(bodies["services/g1/gitea.sqlite3"]) ||
		string(d.staged["goforge-restore/data.tar.gz"]) != string(bodies["services/g1/data.tar.gz"]) {
		t.Errorf("staged = %v", d.staged)
	}
	if d.helper.VolumesFrom != "cid" || d.helper.Image != "sha256:img" || d.helper.Entrypoint[0] != "sleep" {
		t.Errorf("helper = %+v", d.helper)
	}
	args := d.execCmds[0]
	if args[len(args)-3] != "gitea" || args[len(args)-2] != "20261101t130000z" || args[len(args)-1] != "/data/custom/g.db" {
		t.Errorf("script args = %v", args[3:])
	}
	if entries, _ := os.ReadDir(r.TempDir); len(entries) != 0 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestServiceRestore_StoppedContainerIsStartedNotStopped(t *testing.T) {
	s := newMemStore()
	putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img"}}
	r, _ := newSvcRestorer(t, s, d)
	p, _ := r.Plan(context.Background(), svcRunKey, "v1")
	if _, err := r.Restore(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(d.calls, ","), "stop") {
		t.Errorf("stopped a stopped container: %v", d.calls)
	}
}

func TestServiceRestore_IntegrityRefusesBeforeStop(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	putServiceRun(t, s)
	mustPut(t, s, ObjectKey(testPrefix, svcRunKey, "services/g1/data.tar.gz"), "tampered")
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img", Running: true}}
	r, _ := newSvcRestorer(t, s, d)
	p, _ := r.Plan(ctx, svcRunKey, "g1")
	if _, err := r.Restore(ctx, p); !errors.Is(err, ErrRestoreIntegrity) {
		t.Fatalf("err = %v", err)
	}
	if got := strings.Join(d.calls, ","); got != "inspect c-g1" {
		t.Errorf("calls after integrity failure = %s", got)
	}
	if entries, _ := os.ReadDir(r.TempDir); len(entries) != 0 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestServiceRestore_LockAndMissingContainer(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img", Running: true}}
	r, _ := newSvcRestorer(t, s, d)
	r.Lock = func(context.Context) (func(), error) { return nil, repositories.ErrBackupLocked }
	p, _ := r.Plan(ctx, svcRunKey, "g1")
	if _, err := r.Preflight(ctx, p); !errors.Is(err, ErrRestoreBackupRunning) {
		t.Errorf("preflight: %v", err)
	}
	if _, err := r.Restore(ctx, p); !errors.Is(err, ErrRestoreBackupRunning) {
		t.Errorf("restore: %v", err)
	}
	if len(d.calls) != 1 { // the preflight inspect only
		t.Errorf("calls = %v", d.calls)
	}

	d2 := &fakeSvcDocker{inspErr: ErrServiceContainerMissing}
	r2, _ := newSvcRestorer(t, s, d2)
	if _, err := r2.Preflight(ctx, p); !errors.Is(err, ErrServiceContainerMissing) || !strings.Contains(err.Error(), "c-g1") {
		t.Errorf("missing container: %v", err)
	}
}

func TestServiceRestore_ScriptFailureRollsBackAndRestarts(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img", Running: true}, execFail: 1}
	r, _ := newSvcRestorer(t, s, d)
	p, _ := r.Plan(ctx, svcRunKey, "v1")
	_, err := r.Restore(ctx, p)
	if err == nil || !strings.Contains(err.Error(), "previous data was put back") || !strings.Contains(err.Error(), ".goforge-failed-restore-20261101t130000z") {
		t.Fatalf("err = %v", err)
	}
	want := "inspect c-v1,stop cid,helper,copy h1 /tmp,exec h1,exec h1,rmhelper h1,start cid"
	if got := strings.Join(d.calls, ","); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	if !strings.Contains(d.execCmds[1][2], "goforge-failed-restore-") {
		t.Error("second exec is not the rollback script")
	}
}

func TestServiceRestore_Unhealthy(t *testing.T) {
	s := newMemStore()
	putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img", Running: true}, unhealthy: true}
	r, _ := newSvcRestorer(t, s, d)
	p, _ := r.Plan(context.Background(), svcRunKey, "v1")
	res, err := r.Restore(context.Background(), p)
	if err == nil || res == nil || res.Healthy || !strings.Contains(err.Error(), res.AsideDir) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

// The restore and rollback scripts run for real against a temp directory
// standing in for /data (no root, no Docker).
func TestRestoreServiceScripts_Local(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("scripts use GNU/busybox stat -c")
	}
	for _, tc := range []struct{ tpl, db, override string }{
		{templateVaultwarden, "db.sqlite3", ""},
		{templateGitea, "gitea/gitea.db", ""},
		{templateGitea, "custom/g.db", "@/custom/g.db"},
		{templateGitea, "gitea/fromini.db", "ini"},
	} {
		t.Run(tc.tpl+"-"+tc.db, func(t *testing.T) {
			root := t.TempDir()
			mnt, src := filepath.Join(root, "data"), filepath.Join(root, "stage")
			must(t, os.MkdirAll(filepath.Join(mnt, filepath.Dir(tc.db)), 0o750))
			must(t, os.WriteFile(filepath.Join(mnt, tc.db), []byte("current-db"), 0o640)) //nolint:gosec // the test checks this mode is carried over
			must(t, os.WriteFile(filepath.Join(mnt, "marker.txt"), []byte("current"), 0o600))
			must(t, os.Mkdir(filepath.Join(mnt, ".goforge-pre-restore-older"), 0o700))
			must(t, os.MkdirAll(src, 0o700))
			files := map[string]string{"marker.txt": "backup"}
			if tc.override == "ini" {
				files["gitea/conf/app.ini"] = "[database]\nDB_TYPE = sqlite3\nPATH = " + filepath.Join(mnt, tc.db) + "\n"
			}
			must(t, os.WriteFile(filepath.Join(src, "data.tar.gz"), tgz(t, files), 0o600))
			must(t, os.WriteFile(filepath.Join(src, "db.sqlite3"), []byte("backup-db"), 0o600))

			override := ""
			if strings.HasPrefix(tc.override, "@") {
				override = mnt + tc.override[1:]
			}
			script := strings.NewReplacer("mnt="+serviceMount, "mnt="+mnt, "src="+restoreStage, "src="+src).Replace(restoreServiceScript)
			out, err := exec.Command("sh", "-c", script, "sh", tc.tpl, "s1", override).CombinedOutput() //nolint:gosec // test-only: the package script against a temp dir
			if err != nil {
				t.Fatalf("restore script: %v\n%s", err, out)
			}
			aside := filepath.Join(mnt, ".goforge-pre-restore-s1")
			assertFile(t, filepath.Join(mnt, "marker.txt"), "backup")
			assertFile(t, filepath.Join(mnt, tc.db), "backup-db")
			assertFile(t, filepath.Join(aside, "marker.txt"), "current")
			assertFile(t, filepath.Join(aside, tc.db), "current-db")
			if st, _ := os.Stat(filepath.Join(mnt, tc.db)); st.Mode().Perm() != 0o640 {
				t.Errorf("db mode = %v, want the previous file's 0640", st.Mode().Perm())
			}
			if _, err := os.Stat(filepath.Join(mnt, ".goforge-pre-restore-older")); err != nil {
				t.Error("an older kept copy was moved")
			}

			if out, err := exec.Command("sh", "-c", rollbackServiceScript, "sh", "s1", mnt).CombinedOutput(); err != nil { //nolint:gosec // test-only: the package script against a temp dir
				t.Fatalf("rollback script: %v\n%s", err, out)
			}
			assertFile(t, filepath.Join(mnt, "marker.txt"), "current")
			assertFile(t, filepath.Join(mnt, tc.db), "current-db")
			assertFile(t, filepath.Join(mnt, ".goforge-failed-restore-s1", "marker.txt"), "backup")
			if _, err := os.Stat(aside); !os.IsNotExist(err) {
				t.Errorf("aside dir still there: %v", err)
			}
		})
	}
}

func TestRestoreServiceScript_RefusesDBOutsideMount(t *testing.T) {
	root := t.TempDir()
	mnt, src := filepath.Join(root, "data"), filepath.Join(root, "stage")
	must(t, os.MkdirAll(mnt, 0o750))
	must(t, os.MkdirAll(src, 0o700))
	must(t, os.WriteFile(filepath.Join(src, "data.tar.gz"), tgz(t, map[string]string{"a": "b"}), 0o600))
	must(t, os.WriteFile(filepath.Join(src, "db.sqlite3"), []byte("x"), 0o600))
	script := strings.NewReplacer("mnt="+serviceMount, "mnt="+mnt, "src="+restoreStage, "src="+src).Replace(restoreServiceScript)
	for _, bad := range []string{"/etc/passwd", mnt + "/../escape.db"} {
		if out, err := exec.Command("sh", "-c", script, "sh", templateGitea, "s-"+filepath.Base(bad), bad).CombinedOutput(); err == nil { //nolint:gosec // test-only: the package script against a temp dir
			t.Errorf("override %q accepted:\n%s", bad, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escape.db")); err == nil {
		t.Error("wrote outside the mount")
	}
}

// putPostgresOnlyRun writes a run whose postgres instance "p2" lacks
// roles.sql.
func putPostgresOnlyRun(t *testing.T, s *memStore, key string) {
	t.Helper()
	b := []byte("pg")
	m := &Manifest{Version: 1, RunKey: key, RunID: "r2", StartedAt: time.Date(2026, 10, 31, 7, 0, 0, 0, time.UTC),
		Artifacts: []ManifestArtifact{{Key: "services/p2/app.dump", Kind: KindPgDump, InstanceID: "p2", Template: templatePostgres, Database: "app", Bytes: 2, SHA256: sha(b)}}}
	mustPut(t, s, ObjectKey(testPrefix, key, "services/p2/app.dump"), string(b))
	body, _ := json.Marshal(m)
	mustPut(t, s, ObjectKey(testPrefix, key, ManifestName), string(body))
}

func TestServiceRestorePlan_PostgresNeedsRoles(t *testing.T) {
	s := newMemStore()
	const key = "2026-10-31/20261031T070000Z"
	putPostgresOnlyRun(t, s, key)
	r, _ := newSvcRestorer(t, s, &fakeSvcDocker{})
	if _, err := r.Plan(context.Background(), key, "p2"); err == nil || !strings.Contains(err.Error(), "roles.sql") {
		t.Fatalf("err = %v", err)
	}
}

func TestServiceRestore_PostgresOrderAndStaging(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	bodies := putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "sha256:pg", Running: true, PostgresUser: "pgadmin"}}
	r, out := newSvcRestorer(t, s, d)
	locked := 0
	r.Lock = func(context.Context) (func(), error) { locked++; return func() { locked-- }, nil }
	p, err := r.Plan(ctx, svcRunKey, "p1")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Restore(ctx, p)
	if err != nil {
		t.Fatalf("Restore: %v\n%s", err, out)
	}
	want := "inspect c-p1,stop cid,helper,copy h1 /tmp,exec h1,rmhelper h1,start cid,healthy cid"
	if got := strings.Join(d.calls, ","); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	if locked != 0 {
		t.Error("lock not released")
	}
	if !res.Healthy || res.AsideDir != "/var/lib/postgresql/data/.goforge-pre-restore-20261101t130000z" {
		t.Errorf("result = %+v", res)
	}
	if len(d.staged) != 3 || string(d.staged["goforge-restore/roles.sql"]) != string(bodies["services/p1/roles.sql"]) ||
		string(d.staged["goforge-restore/0.dump"]) != string(bodies["services/p1/app.dump"]) ||
		string(d.staged["goforge-restore/1.dump"]) != string(bodies["services/p1/postgres.dump"]) {
		t.Errorf("staged = %v", d.staged)
	}
	cmd := d.execCmds[0]
	if cmd[2] != restorePostgresScript || strings.Join(cmd[3:], " ") != "sh /var/lib/postgresql/data 20261101t130000z pgadmin app postgres" {
		t.Errorf("script args = %v", cmd[3:])
	}
	if entries, _ := os.ReadDir(r.TempDir); len(entries) != 0 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestServiceRestore_PostgresRollbackAndUser(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	putServiceRun(t, s)
	d := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img", Running: true}, execFail: 1}
	r, _ := newSvcRestorer(t, s, d)
	p, _ := r.Plan(ctx, svcRunKey, "p1")
	_, err := r.Restore(ctx, p)
	if err == nil || !strings.Contains(err.Error(), "/var/lib/postgresql/data/.goforge-failed-restore-20261101t130000z") {
		t.Fatalf("err = %v", err)
	}
	if got := d.execCmds[0][6]; got != defaultPostgresSuperuser {
		t.Errorf("superuser without POSTGRES_USER = %q", got)
	}
	if rb := d.execCmds[1]; rb[2] != rollbackServiceScript || rb[4] != "20261101t130000z" || rb[5] != "/var/lib/postgresql/data" {
		t.Errorf("rollback = %v", rb[3:])
	}
	if !strings.HasSuffix(strings.Join(d.calls, ","), "rmhelper h1,start cid") {
		t.Errorf("calls = %v", d.calls)
	}

	bad := &fakeSvcDocker{info: &ServiceContainerInfo{ID: "cid", Image: "img", Running: true, PostgresUser: "-x"}}
	r2, _ := newSvcRestorer(t, s, bad)
	if _, err := r2.Preflight(ctx, p); err == nil || !strings.Contains(err.Error(), "POSTGRES_USER") {
		t.Errorf("bad user preflight: %v", err)
	}
}

// The Postgres script refuses before it moves anything when the image
// lacks its tools, and masks password hashes in what it prints.
func TestRestorePostgresScript_Local(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("scripts use GNU/busybox stat -c")
	}
	root := t.TempDir()
	mnt, src := filepath.Join(root, "pgdata"), filepath.Join(root, "stage")
	must(t, os.MkdirAll(mnt, 0o700))
	must(t, os.WriteFile(filepath.Join(mnt, "PG_VERSION"), []byte("17"), 0o600))
	must(t, os.MkdirAll(src, 0o700))
	must(t, os.WriteFile(filepath.Join(src, "roles.sql"), []byte("roles"), 0o600))
	script := strings.Replace(restorePostgresScript, "src="+restoreStage, "src="+src, 1)

	// A missing dump for a named database.
	cmd := exec.Command("sh", "-c", script, "sh", mnt, "s1", "pgadmin", "app") //nolint:gosec // test-only: the package script against a temp dir
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "staged artifacts are missing") {
		t.Errorf("missing dump: %v %s", err, out)
	}
	must(t, os.WriteFile(filepath.Join(src, "0.dump"), []byte("dump"), 0o600))
	// No initdb/gosu on PATH: refused before the move.
	cmd = exec.Command("sh", "-c", script, "sh", mnt, "s1", "pgadmin", "app") //nolint:gosec // test-only: the package script against a temp dir
	cmd.Env = []string{"PATH=" + filepath.Dir(mustLook(t, "sh")) + ":/nonexistent"}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "is not in the service image") {
		t.Errorf("no tools: %v %s", err, out)
	}
	assertFile(t, filepath.Join(mnt, "PG_VERSION"), "17")
	if _, err := os.Stat(filepath.Join(mnt, ".goforge-pre-restore-s1")); !os.IsNotExist(err) {
		t.Errorf("aside dir created before the checks: %v", err)
	}

	var redact string
	for _, line := range strings.Split(restorePostgresScript, "\n") {
		if strings.HasPrefix(line, "redact()") {
			redact = line
		}
	}
	errFile := filepath.Join(root, "err")
	must(t, os.WriteFile(errFile, []byte("ERROR: x at ALTER ROLE a PASSWORD 'SCRAM-SHA-256$4096:abc$def:ghi'\nmd50123456789abcdef0123456789abcdef\nPASSWORD 'plain'\n"), 0o600))
	out, err := exec.Command("sh", "-c", redact+"\nredact \"$1\" 2>&1", "sh", errFile).CombinedOutput() //nolint:gosec // test-only: the package function against a temp file
	if err != nil {
		t.Fatalf("redact: %v %s", err, out)
	}
	for _, leak := range []string{"abc", "0123456789abcdef", "plain"} {
		if strings.Contains(string(out), leak) {
			t.Errorf("redact leaked %q:\n%s", leak, out)
		}
	}
}

func mustLook(t *testing.T, bin string) string {
	t.Helper()
	p, err := exec.LookPath(bin)
	if err != nil {
		t.Skipf("%s not on PATH", bin)
	}
	return p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Errorf("%s = %q, %v; want %q", path, b, err, want)
	}
}

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		must(t, tw.WriteHeader(&tar.Header{Name: "./" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(body))
		must(t, err)
	}
	must(t, tw.Close())
	must(t, gz.Close())
	return buf.Bytes()
}
