//go:build integration

// Service restore round trips against a throwaway MinIO and throwaway
// Gitea, Vaultwarden, and Postgres containers: back up, change the data,
// restore-service, and check the backed-up state is back and the changed
// state is kept aside. Every container, volume, and network they create is
// named gfsr-it-* and removed afterwards; they never look at other
// containers.
//
// Run with Docker (and sqlite3 on PATH for the Gitea/Vaultwarden test):
//
//	go test -tags integration -run Integration_RestoreService ./internal/backup/
package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

const (
	srAccessKey = "gfsradmin"
	srSecretKey = "gfsr-it-secret-key-0123456789" //nolint:gosec // throwaway MinIO credential, test only
	srBucket    = "gfsr-it"
	srPrefix    = "it/"
	srUserPass  = "gfsr-it-Passw0rd-xyz" //nolint:gosec // throwaway Gitea user password, test only
	// srPgImage is what the postgres template deploys by default.
	srPgImage   = "postgres:17"
	srPgAdmin   = "pgadmin"
	srPgPass    = "gfsr-it-pg-admin-Passw0rd" //nolint:gosec // throwaway Postgres superuser password, test only
	srAppPass   = "gfsr-it-pg-app-Passw0rd"   //nolint:gosec // throwaway Postgres app role password, test only
	srPgDataDir = "/var/lib/postgresql/data"
)

func TestIntegration_RestoreService(t *testing.T) {
	for _, bin := range []string{"docker", "sqlite3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	name := func(s string) string { return "gfsr-it-" + s + "-" + suffix }

	minioName := name("minio")
	startContainer(t, minioName, "-e", "MINIO_ROOT_USER="+srAccessKey, "-e", "MINIO_ROOT_PASSWORD="+srSecretKey,
		"-p", "127.0.0.1::9000", itMinioImage, "server", "/data")
	endpoint := "127.0.0.1:" + strings.TrimPrefix(strings.Split(dockerCLI(t, "port", minioName, "9000"), "\n")[0], "127.0.0.1:")
	mc, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(srAccessKey, srSecretKey, ""), BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "minio", minioName, func() bool { _, err := mc.ListBuckets(ctx); return err == nil })
	if err := mc.MakeBucket(ctx, srBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}

	// Gitea and Vaultwarden on throwaway volumes, with the templates' health checks.
	giteaName, giteaVol := name("gitea"), name("gitea-vol")
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", giteaVol).Run() }) //nolint:gosec // test-only CLI call with test-controlled args
	startContainer(t, giteaName, "-v", giteaVol+":/data", "-e", "USER_UID=1000", "-e", "USER_GID=1000",
		"-e", "GITEA__database__DB_TYPE=sqlite3", "-e", "GITEA__security__INSTALL_LOCK=true",
		"--health-cmd", "wget --spider -q http://localhost:3000/api/healthz", "--health-interval", "2s", "--health-retries", "30",
		itGiteaImage)
	giteaSQL := func(q string) string {
		t.Helper()
		out, err := exec.Command("docker", "exec", "-u", "git", giteaName, "sqlite3", "-readonly", "/data/gitea/gitea.db", q).CombinedOutput() //nolint:gosec // test-only CLI call with test-controlled args
		if err != nil {
			t.Fatalf("gitea sqlite %q: %v %s", q, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	waitFor(t, "gitea healthy", giteaName, func() bool {
		return dockerCLI(t, "inspect", "-f", "{{.State.Health.Status}}", giteaName) == "healthy"
	})
	giteaUser := func(u string) {
		t.Helper()
		dockerCLI(t, "exec", "-u", "git", giteaName, "gitea", "admin", "user", "create", "--username", u,
			"--password", srUserPass, "--email", u+"@example.invalid", "--must-change-password=false")
	}
	giteaUser("before-backup")
	dockerCLI(t, "exec", "-u", "git", giteaName, "sh", "-c", "echo before > /data/gitea/gfsr-marker.txt")

	vwName, vwVol := name("vw"), name("vw-vol")
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", vwVol).Run() }) //nolint:gosec // test-only CLI call with test-controlled args
	startContainer(t, vwName, "-v", vwVol+":/data",
		"--health-cmd", "curl -fsS http://localhost:80/alive", "--health-interval", "2s", "--health-retries", "30",
		itVWImage)
	waitFor(t, "vaultwarden healthy", vwName, func() bool {
		return dockerCLI(t, "inspect", "-f", "{{.State.Health.Status}}", vwName) == "healthy"
	})
	// Vaultwarden's image has no sqlite3: change its database with the
	// Gitea image's sqlite3 in a one-off container while it is stopped.
	vwSQL := func(q string) string {
		t.Helper()
		dockerCLI(t, "stop", vwName)
		out := dockerCLI(t, "run", "--rm", "--network", "none", "--volumes-from", vwName, "--entrypoint", "sqlite3", itGiteaImage, "/data/db.sqlite3", q)
		dockerCLI(t, "start", vwName)
		waitFor(t, "vaultwarden healthy", vwName, func() bool {
			return dockerCLI(t, "inspect", "-f", "{{.State.Health.Status}}", vwName) == "healthy"
		})
		return out
	}
	vwSQL("CREATE TABLE gfsr_marker(v TEXT); INSERT INTO gfsr_marker VALUES ('before-backup');")
	dockerCLI(t, "exec", vwName, "sh", "-c", "echo before > /data/gfsr-marker.txt")

	containers := map[string]string{"gt1": giteaName, "vw1": vwName}
	dc, err := docker.NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	store, err := NewS3Store(config.BackupConfig{Endpoint: "http://" + endpoint, Region: "us-east-1", Bucket: srBucket, Prefix: srPrefix,
		AccessKeyID: srAccessKey, SecretAccessKey: srSecretKey, ForcePathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	red := NewRedactor(srSecretKey, srAccessKey)
	runner := &Runner{
		Store: store, Runs: &memRuns{}, Prefix: srPrefix, Retention: 5, Location: time.UTC, Timeout: 10 * time.Minute,
		Version: "it", Redactor: red,
		Sources: []Source{&InstanceSource{
			Docker: DockerExecer{Client: dc}, Redactor: red,
			Container: func(id string) string { return containers[id] },
			Instances: fakeLister{{ID: "gt1", TemplateName: "gitea", Name: "git"}, {ID: "vw1", TemplateName: "vaultwarden", Name: "vw"}},
		}},
	}
	run, err := runner.Run(ctx, models.BackupTriggerManual)
	if err != nil || run.Status != models.BackupRunSucceeded {
		t.Fatalf("backup run: %v %+v", err, run)
	}
	t.Logf("backup run %s: objects=%d bytes=%d", run.RunKey, run.Objects, run.Bytes)

	// Change both services after the backup.
	giteaUser("after-backup")
	dockerCLI(t, "exec", "-u", "git", giteaName, "sh", "-c", "echo after > /data/gitea/gfsr-marker.txt")
	vwSQL("UPDATE gfsr_marker SET v = 'after-backup';")
	dockerCLI(t, "exec", vwName, "sh", "-c", "echo after > /data/gfsr-marker.txt; echo new > /data/gfsr-only-after.txt")
	if got := giteaSQL("select count(*) from user where lower_name in ('before-backup','after-backup')"); got != "2" {
		t.Fatalf("gitea users before restore = %s", got)
	}

	var progress bytes.Buffer
	sr := &ServiceRestorer{
		Store: store, Prefix: srPrefix, Docker: DockerServiceOps{Client: dc, Poll: time.Second},
		Container: func(id string) string { return containers[id] }, TempDir: t.TempDir(),
		Redactor: red, Progress: &progress, HealthTimeout: 3 * time.Minute,
	}
	restore := func(id string) *ServiceRestoreResult {
		t.Helper()
		plan, err := sr.Plan(ctx, run.RunKey, id)
		if err != nil {
			t.Fatal(err)
		}
		// Dry run: preflight only, nothing stopped.
		before := dockerCLI(t, "inspect", "-f", "{{.State.StartedAt}}", containers[id])
		if _, err := sr.Preflight(ctx, plan); err != nil {
			t.Fatal(err)
		}
		if after := dockerCLI(t, "inspect", "-f", "{{.State.StartedAt}}", containers[id]); after != before {
			t.Fatal("preflight restarted the container")
		}
		res, err := sr.Restore(ctx, plan)
		if err != nil {
			t.Fatalf("restore %s: %v\n%s", id, err, progress.String())
		}
		if !res.Healthy {
			t.Fatalf("restore %s: not healthy", id)
		}
		t.Logf("restored %s, previous data in %s", id, res.AsideDir)
		return res
	}

	// Gitea: the user created after the backup is gone, the one before is back.
	gres := restore("gt1")
	if got := giteaSQL("select group_concat(lower_name) from (select lower_name from user where lower_name in ('before-backup','after-backup') order by lower_name)"); got != "before-backup" {
		t.Errorf("gitea users after restore = %q, want before-backup", got)
	}
	if got := dockerCLI(t, "exec", giteaName, "cat", "/data/gitea/gfsr-marker.txt"); got != "before" {
		t.Errorf("gitea marker = %q", got)
	}
	if got := dockerCLI(t, "exec", giteaName, "cat", gres.AsideDir+"/gitea/gfsr-marker.txt"); got != "after" {
		t.Errorf("gitea kept marker = %q", got)
	}
	if got := dockerCLI(t, "exec", giteaName, "stat", "-c", "%u:%g", "/data/gitea/gitea.db", "/data/gitea/conf/app.ini", "/data/git"); got != "1000:1000\n1000:1000\n1000:1000" {
		t.Errorf("gitea owners after restore:\n%s", got)
	}
	if out, err := exec.Command("docker", "exec", "-u", "git", giteaName, "sqlite3", "/data/gitea/gitea.db", "PRAGMA integrity_check").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "ok" { //nolint:gosec // test-only CLI call with test-controlled args
		t.Errorf("gitea db integrity: %v %s", err, out)
	}
	// Gitea can write to the restored database.
	giteaUser("after-restore")

	// Vaultwarden: the row and marker are back, the file created after is only in the kept copy.
	vres := restore("vw1")
	if got := vwSQL("SELECT v FROM gfsr_marker;"); got != "before-backup" {
		t.Errorf("vaultwarden marker row = %q", got)
	}
	if got := dockerCLI(t, "exec", vwName, "cat", "/data/gfsr-marker.txt"); got != "before" {
		t.Errorf("vaultwarden marker = %q", got)
	}
	if execOK(vwName, "test", "-e", "/data/gfsr-only-after.txt") {
		t.Error("a file created after the backup is still in /data")
	}
	if got := dockerCLI(t, "exec", vwName, "cat", vres.AsideDir+"/gfsr-only-after.txt"); got != "new" {
		t.Errorf("vaultwarden kept copy = %q", got)
	}

	// No helper container is left behind.
	if out := dockerCLI(t, "ps", "-a", "--filter", "label=goforge.restore-helper", "--format", "{{.Names}}"); strings.Contains(out, suffix) {
		t.Errorf("helper containers left: %s", out)
	}
	// The next backup leaves the kept copies out.
	run2, err := runner.Run(ctx, models.BackupTriggerManual)
	if err != nil || run2.Status != models.BackupRunSucceeded {
		t.Fatalf("second backup run: %v %+v", err, run2)
	}
	for _, id := range []string{"gt1", "vw1"} {
		o, err := mc.GetObject(ctx, srBucket, srPrefix+run2.RunKey+"/services/"+id+"/data.tar.gz", minio.GetObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(o); err != nil {
			t.Fatal(err)
		}
		o.Close()
		for n := range tarNames(t, buf.Bytes(), srSecretKey) {
			if strings.Contains(n, preRestorePrefix) {
				t.Errorf("%s data.tar.gz includes the kept copy: %s", id, n)
				break
			}
		}
	}
	if strings.Contains(progress.String(), srSecretKey) {
		t.Error("progress output contains the bucket secret")
	}
	t.Logf("progress:\n%s", progress.String())
}

// startSRMinio starts a throwaway MinIO with the srBucket and returns a
// client and a Store on it.
func startSRMinio(t *testing.T, ctx context.Context, name string) (*minio.Client, *S3Store) {
	t.Helper()
	startContainer(t, name, "-e", "MINIO_ROOT_USER="+srAccessKey, "-e", "MINIO_ROOT_PASSWORD="+srSecretKey,
		"-p", "127.0.0.1::9000", itMinioImage, "server", "/data")
	endpoint := "127.0.0.1:" + strings.TrimPrefix(strings.Split(dockerCLI(t, "port", name, "9000"), "\n")[0], "127.0.0.1:")
	mc, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(srAccessKey, srSecretKey, ""), BucketLookup: minio.BucketLookupPath})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "minio", name, func() bool { _, err := mc.ListBuckets(ctx); return err == nil })
	if err := mc.MakeBucket(ctx, srBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	store, err := NewS3Store(config.BackupConfig{Endpoint: "http://" + endpoint, Region: "us-east-1", Bucket: srBucket, Prefix: srPrefix,
		AccessKeyID: srAccessKey, SecretAccessKey: srSecretKey, ForcePathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return mc, store
}

// keptClusterQuery starts a throwaway server on a copy of a kept cluster
// ($1) in a one-off container and prints two answers: the app items and
// whether after_db exists.
const keptClusterQuery = `set -e
cp -a "$1" /tmp/kept && chown -R postgres:postgres /tmp/kept && chmod 700 /tmp/kept && rm -f /tmp/kept/postmaster.pid
gosu postgres pg_ctl -D /tmp/kept -w -o "-c listen_addresses='' -c unix_socket_directories=/tmp" start >/dev/null
gosu postgres psql -X -qtA -h /tmp -U pgadmin -d app -c "SELECT string_agg(v, ',' ORDER BY v) FROM items"
gosu postgres psql -X -qtA -h /tmp -U pgadmin -d postgres -c "SELECT count(*) FROM pg_database WHERE datname = 'after_db'"
gosu postgres pg_ctl -D /tmp/kept -m fast -w stop >/dev/null`

func TestIntegration_RestoreServicePostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	name := func(s string) string { return "gfsr-it-" + s + "-" + suffix }
	mc, store := startSRMinio(t, ctx, name("minio"))

	// A Postgres service as the template deploys it, on its own network so
	// a client container can log in with a password over TCP.
	netName := name("net")
	dockerCLI(t, "network", "create", netName)
	t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", netName).Run() }) //nolint:gosec // test-only CLI call with test-controlled args
	pgName, pgVol := name("pg"), name("pg-vol")
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", pgVol).Run() }) //nolint:gosec // test-only CLI call with test-controlled args
	startContainer(t, pgName, "--network", netName, "-v", pgVol+":"+srPgDataDir,
		"-e", "POSTGRES_DB=app", "-e", "POSTGRES_USER="+srPgAdmin, "-e", "POSTGRES_PASSWORD="+srPgPass,
		"--health-cmd", `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`, "--health-interval", "2s", "--health-retries", "30",
		srPgImage)
	psql := func(db, q string) string {
		t.Helper()
		return dockerCLI(t, "exec", pgName, "psql", "-X", "-qtA", "-v", "ON_ERROR_STOP=1", "-U", srPgAdmin, "-d", db, "-c", q)
	}
	pgReady := func() bool {
		return dockerCLI(t, "inspect", "-f", "{{.State.Health.Status}}", pgName) == "healthy" &&
			execOK(pgName, "psql", "-X", "-qtA", "-U", srPgAdmin, "-d", "app", "-c", "SELECT 1")
	}
	// The image's first start runs a socket-only init server, then restarts.
	waitFor(t, "postgres init", pgName, func() bool {
		logs, _ := exec.Command("docker", "logs", pgName).CombinedOutput() //nolint:gosec // test-only CLI call with test-controlled args
		return strings.Contains(string(logs), "PostgreSQL init process complete") && pgReady()
	})
	// tcpLogin runs psql in a client container on the network, over TCP
	// with a password (pg_hba scram-sha-256), and reports success.
	tcpLogin := func(user, pass, db, q string) (string, bool) {
		out, err := exec.Command("docker", "run", "--rm", "--network", netName, "-e", "PGPASSWORD="+pass, srPgImage, //nolint:gosec // test-only CLI call with test-controlled args
			"psql", "-X", "-qtA", "-h", pgName, "-U", user, "-d", db, "-c", q).CombinedOutput()
		return strings.TrimSpace(string(out)), err == nil
	}

	psql("app", "CREATE ROLE app_rw LOGIN PASSWORD '"+srAppPass+"'; CREATE TABLE items(v text); ALTER TABLE items OWNER TO app_rw; INSERT INTO items VALUES ('before-backup');")
	psql("postgres", "CREATE DATABASE other OWNER app_rw")
	psql("other", "CREATE TABLE notes(n int); INSERT INTO notes VALUES (1);")

	containers := map[string]string{"pg1": pgName}
	dc, err := docker.NewClient("")
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()
	red := NewRedactor(srSecretKey, srAccessKey, srPgPass, srAppPass)
	runner := &Runner{
		Store: store, Runs: &memRuns{}, Prefix: srPrefix, Retention: 5, Location: time.UTC, Timeout: 10 * time.Minute,
		Version: "it", Redactor: red,
		Sources: []Source{&InstanceSource{
			Docker: DockerExecer{Client: dc}, Redactor: red,
			Container: func(id string) string { return containers[id] },
			Instances: fakeLister{{ID: "pg1", TemplateName: "postgres", Name: "db"}},
		}},
	}
	run, err := runner.Run(ctx, models.BackupTriggerManual)
	if err != nil || run.Status != models.BackupRunSucceeded {
		t.Fatalf("backup run: %v %+v", err, run)
	}
	t.Logf("backup run %s: objects=%d bytes=%d", run.RunKey, run.Objects, run.Bytes)

	// Change the service after the backup: rows, a new database, a password.
	psql("app", "UPDATE items SET v = 'after-backup'; INSERT INTO items VALUES ('only-after');")
	psql("postgres", "CREATE DATABASE after_db")
	psql("postgres", "ALTER ROLE app_rw PASSWORD 'changed-after-backup'")

	var progress bytes.Buffer
	sr := &ServiceRestorer{
		Store: store, Prefix: srPrefix, Docker: DockerServiceOps{Client: dc, Poll: time.Second},
		Container: func(id string) string { return containers[id] }, TempDir: t.TempDir(),
		Redactor: red, Progress: &progress, HealthTimeout: 3 * time.Minute,
	}
	plan, err := sr.Plan(ctx, run.RunKey, "pg1")
	if err != nil {
		t.Fatal(err)
	}
	var dbs []string
	for _, d := range plan.Dumps {
		dbs = append(dbs, d.Database)
	}
	if got := strings.Join(dbs, ","); got != "app,other,postgres" || plan.Mount != srPgDataDir {
		t.Fatalf("plan databases = %s, mount %s", got, plan.Mount)
	}
	// Dry run: preflight only, nothing stopped.
	before := dockerCLI(t, "inspect", "-f", "{{.State.StartedAt}}", pgName)
	info, err := sr.Preflight(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if info.PostgresUser != srPgAdmin {
		t.Errorf("PostgresUser = %q", info.PostgresUser)
	}
	if after := dockerCLI(t, "inspect", "-f", "{{.State.StartedAt}}", pgName); after != before {
		t.Fatal("preflight restarted the container")
	}
	start := time.Now()
	res, err := sr.Restore(ctx, plan)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, progress.String())
	}
	if !res.Healthy || !strings.HasPrefix(res.AsideDir, srPgDataDir+"/.goforge-pre-restore-") {
		t.Fatalf("result = %+v", res)
	}
	t.Logf("restored pg1 in %s, previous cluster in %s", time.Since(start).Round(time.Second), res.AsideDir)
	waitFor(t, "postgres after restore", pgName, pgReady)

	// The backed-up state is back.
	if got := psql("app", "SELECT string_agg(v, ',' ORDER BY v) FROM items"); got != "before-backup" {
		t.Errorf("items after restore = %q, want before-backup", got)
	}
	if got := psql("postgres", "SELECT count(*) FROM pg_database WHERE datname = 'after_db'"); got != "0" {
		t.Errorf("after_db still exists after restore")
	}
	if got := psql("other", "SELECT n FROM notes"); got != "1" {
		t.Errorf("other.notes = %q", got)
	}
	if got := psql("app", "SELECT tableowner FROM pg_tables WHERE tablename = 'items'"); got != "app_rw" {
		t.Errorf("items owner = %q", got)
	}
	if got := psql("postgres", "SELECT pg_catalog.pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'other'"); got != "app_rw" {
		t.Errorf("other owner = %q", got)
	}
	// Passwords are the backed-up ones, over TCP through the kept pg_hba.conf.
	if out, ok := tcpLogin("app_rw", srAppPass, "app", "SELECT count(*) FROM items"); !ok || out != "1" {
		t.Errorf("app_rw login with the backed-up password: %v %s", ok, out)
	}
	if _, ok := tcpLogin("app_rw", "changed-after-backup", "app", "SELECT 1"); ok {
		t.Error("app_rw logged in with the password set after the backup")
	}
	if _, ok := tcpLogin(srPgAdmin, srPgPass, "postgres", "SELECT 1"); !ok {
		t.Error("superuser login over TCP failed after restore")
	}
	if _, ok := tcpLogin(srPgAdmin, "wrong-password", "postgres", "SELECT 1"); ok {
		t.Error("superuser logged in with a wrong password")
	}
	// Owners: the cluster belongs to the image's postgres user.
	if got := dockerCLI(t, "exec", pgName, "stat", "-c", "%U:%a", srPgDataDir+"/PG_VERSION", srPgDataDir, srPgDataDir+"/base", res.AsideDir); got != "postgres:600\npostgres:700\npostgres:700\npostgres:700" {
		t.Errorf("owners after restore:\n%s", got)
	}
	if execOK(pgName, "sh", "-c", "ls -d "+srPgDataDir+"/.goforge-restore-new-* 2>/dev/null | grep -q .") {
		t.Error("the build directory was left in PGDATA")
	}
	// The kept folder is the changed cluster: start it on a copy and read it.
	kept := dockerCLI(t, "run", "--rm", "--network", "none", "--volumes-from", pgName, "--entrypoint", "sh", srPgImage, "-c", keptClusterQuery, "sh", res.AsideDir)
	if kept != "after-backup,only-after\n1" {
		t.Errorf("kept cluster answers:\n%s\nwant after-backup,only-after and after_db present", kept)
	}
	// The service accepts writes.
	psql("app", "INSERT INTO items VALUES ('after-restore')")

	// Rollback: a run whose app dump verifies but is not a pg_dump archive.
	badKey := "2099-01-01/20990101T000000Z"
	bad := *plan.Manifest
	bad.RunKey = badKey
	bad.Artifacts = nil
	for _, a := range plan.Manifest.Artifacts {
		body := []byte("this is not a pg_dump archive")
		if a.Key != "services/pg1/app.dump" {
			o, gerr := store.Get(ctx, ObjectKey(srPrefix, run.RunKey, a.Key))
			if gerr != nil {
				t.Fatal(gerr)
			}
			var buf bytes.Buffer
			if _, rerr := buf.ReadFrom(o); rerr != nil {
				t.Fatal(rerr)
			}
			o.Close()
			body = buf.Bytes()
		}
		a.Bytes, a.SHA256 = int64(len(body)), sha256Hex(body)
		bad.Artifacts = append(bad.Artifacts, a)
		if perr := store.Put(ctx, ObjectKey(srPrefix, badKey, a.Key), bytes.NewReader(body), "application/octet-stream"); perr != nil {
			t.Fatal(perr)
		}
	}
	mb, err := json.Marshal(&bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, ObjectKey(srPrefix, badKey, ManifestName), bytes.NewReader(mb), "application/json"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // a new UTC-second stamp for the second restore
	badPlan, err := sr.Plan(ctx, badKey, "pg1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sr.Restore(ctx, badPlan)
	if err == nil || !strings.Contains(err.Error(), "the previous data was put back") || !strings.Contains(err.Error(), srPgDataDir+"/.goforge-failed-restore-") {
		t.Fatalf("bad dump restore err = %v\n%s", err, progress.String())
	}
	t.Logf("bad dump restore refused as expected: %v", err)
	waitFor(t, "postgres after rollback", pgName, pgReady)
	if got := psql("app", "SELECT string_agg(v, ',' ORDER BY v) FROM items"); got != "after-restore,before-backup" {
		t.Errorf("items after rollback = %q, want after-restore,before-backup", got)
	}
	if got := dockerCLI(t, "exec", pgName, "sh", "-c", "ls -d "+srPgDataDir+"/.goforge-failed-restore-*/.goforge-restore-new-* "+srPgDataDir+"/.goforge-pre-restore-* | wc -l"); got != "2" {
		t.Errorf("kept folders after rollback: %s", got)
	}

	if out := dockerCLI(t, "ps", "-a", "--filter", "label=goforge.restore-helper", "--format", "{{.Names}}"); strings.Contains(out, suffix) {
		t.Errorf("helper containers left: %s", out)
	}
	for _, secret := range []string{srSecretKey, srPgPass, srAppPass, "SCRAM-SHA-256$"} {
		if strings.Contains(progress.String(), secret) || strings.Contains(err.Error(), secret) {
			t.Errorf("progress or error contains a secret (%d chars)", len(secret))
		}
	}
	_ = mc
	t.Logf("progress:\n%s", progress.String())
}
