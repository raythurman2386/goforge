//go:build integration

// Integration test against a throwaway MinIO and throwaway postgres, Gitea,
// and Vaultwarden containers. Every container and volume it creates is
// named gfbk-it-* and removed afterwards; it never looks at other
// containers.
//
// Run (needs Docker, local pg_restore and sqlite3, and a goforge test
// database in DATABASE_URL for the app-db target):
//
//	DATABASE_URL=postgres://... go test -tags integration -run Integration ./internal/backup/
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	itMinioImage = "cgr.dev/chainguard/minio:latest"
	itPgImage    = "postgres:17-alpine"
	itGiteaImage = "gitea/gitea:1.25.5"
	itVWImage    = "vaultwarden/server:latest"
	itAccessKey  = "gfbkadmin"
	itSecretKey  = "gfbk-it-secret-key-0123456789" //nolint:gosec // throwaway MinIO credential, test only
	itEncKey     = "gfbk-it-ENCRYPTION-KEY-must-never-appear-4f2a9c"
	itBucket     = "gfbk-it"
	itPrefix     = "it/"
)

func dockerCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func startContainer(t *testing.T, name string, args ...string) {
	t.Helper()
	_ = exec.Command("docker", "rm", "-f", "-v", name).Run()
	dockerCLI(t, append([]string{"run", "-d", "--name", name}, args...)...)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", "-v", name).Run() })
}

func waitFor(t *testing.T, what, container string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Second)
	}
	logs, _ := exec.Command("docker", "logs", "--tail", "30", container).CombinedOutput()
	t.Fatalf("timed out waiting for %s; logs:\n%s", what, logs)
}

func execOK(name string, cmd ...string) bool {
	return exec.Command("docker", append([]string{"exec", name}, cmd...)...).Run() == nil //nolint:gosec // test-only CLI call with test-controlled args
}

func TestIntegration_RunAgainstMinIO(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	for _, bin := range []string{"docker", "pg_restore", "sqlite3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	t.Setenv("ENCRYPTION_KEY", itEncKey)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	name := func(s string) string { return "gfbk-it-" + s + "-" + suffix }

	// MinIO
	minioName := name("minio")
	startContainer(t, minioName, "-e", "MINIO_ROOT_USER="+itAccessKey, "-e", "MINIO_ROOT_PASSWORD="+itSecretKey,
		"-p", "127.0.0.1::9000", itMinioImage, "server", "/data")
	hostPort := strings.TrimPrefix(strings.Split(dockerCLI(t, "port", minioName, "9000"), "\n")[0], "127.0.0.1:")
	endpoint := "127.0.0.1:" + hostPort
	mc, mcErr := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(itAccessKey, itSecretKey, ""), BucketLookup: minio.BucketLookupPath})
	if mcErr != nil {
		t.Fatal(mcErr)
	}
	waitFor(t, "minio", minioName, func() bool { _, err := mc.ListBuckets(ctx); return err == nil })
	if err := mc.MakeBucket(ctx, itBucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mc.PutObject(ctx, itBucket, itPrefix+"unrelated.txt", strings.NewReader("keep"), 4, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}

	// Postgres instance with a second database and an extra role.
	pgName := name("pg")
	startContainer(t, pgName, "-e", "POSTGRES_USER=tenant", "-e", "POSTGRES_PASSWORD=tenant-pw-xyz", "-e", "POSTGRES_DB=tenant", itPgImage)
	waitFor(t, "postgres", pgName, func() bool {
		return execOK(pgName, "psql", "-U", "tenant", "-d", "tenant", "-c", "select 1")
	})
	dockerCLI(t, "exec", pgName, "psql", "-U", "tenant", "-d", "tenant", "-c", `CREATE DATABASE "second db"`)
	dockerCLI(t, "exec", pgName, "psql", "-U", "tenant", "-d", "tenant", "-c", `CREATE ROLE gfbk_app LOGIN PASSWORD 'x'; CREATE TABLE t(id int); INSERT INTO t VALUES (1),(2)`)

	// Gitea (sqlite) and Vaultwarden, each on a throwaway volume.
	giteaName, giteaVol := name("gitea"), name("gitea-vol")
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", giteaVol).Run() }) //nolint:gosec // test-only CLI call with test-controlled args
	startContainer(t, giteaName, "-v", giteaVol+":/data", "-e", "GITEA__database__DB_TYPE=sqlite3",
		"-e", "GITEA__security__INSTALL_LOCK=true", itGiteaImage)
	waitFor(t, "gitea db", giteaName, func() bool {
		// Probe as git, read-only, and only once the file exists: sqlite3
		// creates a missing database, and a root-owned one breaks Gitea.
		out, err := exec.Command("docker", "exec", "-u", "git", giteaName, "sh", "-c", //nolint:gosec // test-only CLI call with test-controlled args
			`test -f /data/gitea/gitea.db && sqlite3 -readonly /data/gitea/gitea.db "select count(*) from version"`).CombinedOutput()
		if err != nil {
			t.Logf("gitea not ready: %v %s", err, bytes.TrimSpace(out))
		}
		return err == nil
	})
	vwName, vwVol := name("vw"), name("vw-vol")
	t.Cleanup(func() { _ = exec.Command("docker", "volume", "rm", "-f", vwVol).Run() }) //nolint:gosec // test-only CLI call with test-controlled args
	startContainer(t, vwName, "-v", vwVol+":/data", itVWImage)
	waitFor(t, "vaultwarden db", vwName, func() bool { return execOK(vwName, "test", "-s", "/data/db.sqlite3") })

	containers := map[string]string{"pg1": pgName, "gt1": giteaName, "vw1": vwName, "pg-stopped": name("does-not-exist")}
	dc, dcErr := docker.NewClient("")
	if dcErr != nil {
		t.Fatal(dcErr)
	}
	defer dc.Close()

	cfg := config.BackupConfig{Endpoint: "http://" + endpoint, Region: "us-east-1", Bucket: itBucket, Prefix: itPrefix,
		AccessKeyID: itAccessKey, SecretAccessKey: itSecretKey, ForcePathStyle: true}
	store, storeErr := NewS3Store(cfg)
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	red := NewRedactor(itEncKey, itSecretKey, itAccessKey)
	chi, _ := time.LoadLocation("America/Chicago")
	runs := &memRuns{}
	runner := &Runner{
		Store: store, Runs: runs, Prefix: itPrefix, Retention: 2, Location: chi, Timeout: 10 * time.Minute,
		Version: "it", Redactor: red,
		Sources: []Source{
			Static{&AppDBTarget{DatabaseURL: dbURL, Redactor: red}},
			&InstanceSource{
				Docker: DockerExecer{Client: dc}, Redactor: red,
				Container: func(id string) string { return containers[id] },
				Instances: fakeLister{
					{ID: "pg1", TemplateName: "postgres", Name: "tenant-pg"},
					{ID: "pg-stopped", TemplateName: "postgres", Name: "gone"},
					{ID: "gt1", TemplateName: "gitea", Name: "git"},
					{ID: "vw1", TemplateName: "vaultwarden", Name: "vw"},
					{ID: "rd1", TemplateName: "redis", Name: "shared-redis"},
				},
			},
		},
	}

	run, runErr := runner.Run(ctx, models.BackupTriggerManual)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if run.Status != models.BackupRunSucceeded {
		t.Fatalf("run failed: %s", run.ErrorSummary.String)
	}
	t.Logf("run %s: objects=%d bytes=%d skipped=%d", run.RunKey, run.Objects, run.Bytes, run.Skipped)
	if run.Skipped != 2 {
		t.Errorf("skipped = %d, want 2 (stopped postgres + redis)", run.Skipped)
	}

	get := func(key string) []byte {
		t.Helper()
		o, err := mc.GetObject(ctx, itBucket, key, minio.GetObjectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer o.Close()
		b, err := io.ReadAll(o)
		if err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
		return b
	}
	runDir := itPrefix + run.RunKey + "/"
	var m Manifest
	if err := json.Unmarshal(get(runDir+ManifestName), &m); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{
		"app/goforge-db.dump",
		"services/pg1/postgres.dump", "services/pg1/second_db.dump", "services/pg1/tenant.dump", "services/pg1/roles.sql",
		"services/gt1/gitea.sqlite3", "services/gt1/data.tar.gz",
		"services/vw1/vaultwarden.sqlite3", "services/vw1/data.tar.gz",
	}
	byKey := map[string]ManifestArtifact{}
	for _, a := range m.Artifacts {
		byKey[a.Key] = a
	}
	if len(m.Artifacts) != len(wantKeys) {
		t.Errorf("manifest has %d artifacts, want %d: %+v", len(m.Artifacts), len(wantKeys), m.Artifacts)
	}

	tmp := t.TempDir()
	for _, k := range wantKeys {
		a, ok := byKey[k]
		if !ok {
			t.Errorf("manifest missing %s", k)
			continue
		}
		body := get(runDir + k)
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != a.SHA256 || int64(len(body)) != a.Bytes {
			t.Errorf("%s: sha256/bytes mismatch with manifest", k)
		}
		if bytes.Contains(body, []byte(itEncKey)) || bytes.Contains(body, []byte(itSecretKey)) {
			t.Errorf("%s contains a GoForge secret", k)
		}
		local := filepath.Join(tmp, strings.ReplaceAll(k, "/", "_"))
		if err := os.WriteFile(local, body, 0o600); err != nil {
			t.Fatal(err)
		}
		switch a.Kind {
		case KindPgDump:
			out, err := exec.Command("pg_restore", "--list", local).CombinedOutput() //nolint:gosec // test-only CLI call with test-controlled args
			if err != nil {
				t.Errorf("pg_restore --list %s: %v\n%s", k, err, out)
			}
			if k == "services/pg1/tenant.dump" && !strings.Contains(string(out), "TABLE DATA public t") {
				t.Errorf("tenant dump has no table t:\n%s", out)
			}
		case KindPgRoles:
			if !strings.Contains(string(body), "CREATE ROLE gfbk_app") {
				t.Errorf("roles.sql missing gfbk_app")
			}
		case KindSQLite:
			out, err := exec.Command("sqlite3", local, "PRAGMA integrity_check").CombinedOutput() //nolint:gosec // test-only CLI call with test-controlled args
			if err != nil || strings.TrimSpace(string(out)) != "ok" {
				t.Errorf("sqlite integrity_check %s: %v %s", k, err, out)
			}
		case KindTarGz:
			names := tarNames(t, body, itEncKey)
			for _, bad := range []string{"./gitea/gitea.db", "./db.sqlite3", "./db.sqlite3-wal"} {
				if names[bad] {
					t.Errorf("%s includes %s", k, bad)
				}
			}
			want := "./gitea/conf/app.ini"
			if strings.HasPrefix(k, "services/vw1/") {
				want = "./rsa_key.pem"
			}
			if !names[want] {
				t.Errorf("%s missing %s", k, want)
			}
		}
	}

	// Vaultwarden's temporary backup file must be gone from the volume.
	if out := dockerCLI(t, "exec", vwName, "sh", "-c", "ls /data/db_*.sqlite3 2>/dev/null || true"); out != "" {
		t.Errorf("vaultwarden backup file left in /data: %s", out)
	}
	if out := dockerCLI(t, "exec", giteaName, "sh", "-c", "ls /tmp/goforge-backup.* 2>/dev/null || true"); out != "" {
		t.Errorf("gitea snapshot temp file left: %s", out)
	}

	// Retention against the real bucket: seed four older complete days and
	// one failed day, then run again with retention 2.
	for _, d := range []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04"} {
		id := strings.ReplaceAll(d, "-", "") + "T080000Z"
		for _, f := range []string{"app/goforge-db.dump", ManifestName} {
			if _, err := mc.PutObject(ctx, itBucket, itPrefix+d+"/"+id+"/"+f, strings.NewReader("x"), 1, minio.PutObjectOptions{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	runner.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	run2, err := runner.Run(ctx, models.BackupTriggerManual)
	if err != nil || run2.Status != models.BackupRunSucceeded {
		t.Fatalf("second run: %v %+v", err, run2)
	}
	if run2.Pruned != 8 {
		t.Errorf("pruned = %d, want 8 (four seeded days x 2 objects)", run2.Pruned)
	}
	left := map[string]bool{}
	for o := range mc.ListObjects(ctx, itBucket, minio.ListObjectsOptions{Prefix: itPrefix, Recursive: true}) {
		left[o.Key] = true
	}
	if !left[itPrefix+"unrelated.txt"] || !left[runDir+ManifestName] || !left[itPrefix+run2.RunKey+"/"+ManifestName] {
		t.Errorf("retention removed something it must keep: %v", left)
	}
	for k := range left {
		if strings.HasPrefix(k, itPrefix+"2026-01-") {
			t.Errorf("retention kept %s", k)
		}
	}
}

func tarNames(t *testing.T, gz []byte, secret string) map[string]bool {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	names := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[h.Name] = true
		b, _ := io.ReadAll(tr)
		if bytes.Contains(b, []byte(secret)) {
			t.Errorf("tar entry %s contains a GoForge secret", h.Name)
		}
	}
	return names
}
