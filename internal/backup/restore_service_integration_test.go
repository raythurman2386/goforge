//go:build integration

// Service restore round trip against a throwaway MinIO and throwaway Gitea
// and Vaultwarden containers: back up, change the data, restore-service,
// and check the backed-up state is back and the changed state is kept
// aside. Every container and volume it creates is named gfsr-it-* and
// removed afterwards; it never looks at other containers.
//
// Run with Docker and sqlite3 on PATH:
//
//	go test -tags integration -run Integration_RestoreService ./internal/backup/
package backup

import (
	"bytes"
	"context"
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
