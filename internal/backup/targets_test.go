package backup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestPgEnvFromURL(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "must-not-leak")
	env, err := pgEnvFromURL("postgres://goforge:p%40ss@db:5432/goforge?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(env, "\n")
	for _, want := range []string{"PGHOST=db", "PGPORT=5432", "PGUSER=goforge", "PGPASSWORD=p@ss", "PGDATABASE=goforge", "PGSSLMODE=disable"} {
		if !strings.Contains(got, want) {
			t.Errorf("env missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ENCRYPTION_KEY") || strings.Contains(got, "must-not-leak") {
		t.Error("pg_dump env carries ENCRYPTION_KEY")
	}
	if _, err := pgEnvFromURL("host=db user=x"); err == nil {
		t.Error("key=value DSN should be rejected")
	}
}

// TestAppDBTarget_EnvIsolation runs a fake pg_dump that prints its
// environment, proving the process env (ENCRYPTION_KEY, S3 keys) is not
// inherited and that args carry no secret.
func TestAppDBTarget_EnvIsolation(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "pg_dump")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho \"ARGS=$*\"\nenv\n"), 0o700); err != nil { //nolint:gosec // test script must be executable
		t.Fatal(err)
	}
	t.Setenv("ENCRYPTION_KEY", "enc-key-value-123")
	t.Setenv("BACKUP_S3_SECRET_ACCESS_KEY", "s3-secret-value-456")
	tg := &AppDBTarget{DatabaseURL: "postgres://u:dbpass@h:1/d", PgDump: fake}
	arts, err := tg.Plan(context.Background())
	if err != nil || len(arts) != 1 {
		t.Fatalf("Plan() = %v, %v", arts, err)
	}
	var out bytes.Buffer
	if err := arts[0].Dump(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "enc-key-value-123") || strings.Contains(s, "s3-secret-value-456") {
		t.Errorf("pg_dump saw a GoForge secret:\n%s", s)
	}
	if !strings.Contains(s, "ARGS=--format=custom --no-password\n") || strings.Contains(s, "ARGS=*dbpass") {
		t.Errorf("unexpected argv:\n%s", s)
	}
	if !strings.Contains(s, "PGPASSWORD=dbpass") {
		t.Errorf("PGPASSWORD not passed:\n%s", s)
	}
}

type fakeExec struct {
	running map[string]bool
	calls   []docker.ExecOptions
	outputs map[string]string // substring of script -> stdout
	exit    map[string]int
}

func (f *fakeExec) Running(_ context.Context, id string) (bool, error) { return f.running[id], nil }

func (f *fakeExec) ExecStream(_ context.Context, _ string, opts docker.ExecOptions, w io.Writer) (docker.ExecStreamResult, error) {
	f.calls = append(f.calls, opts)
	script := strings.Join(opts.Cmd, " ")
	for sub, out := range f.outputs {
		if strings.Contains(script, sub) {
			n, err := io.WriteString(w, out)
			return docker.ExecStreamResult{Bytes: int64(n), ExitCode: f.exit[sub], Stderr: "stderr with postgres://a:b@c"}, err
		}
	}
	return docker.ExecStreamResult{}, nil
}

type fakeLister []*models.ServiceInstance

func (f fakeLister) ListAll(context.Context) ([]*models.ServiceInstance, error) { return f, nil }

func TestInstanceSource(t *testing.T) {
	ex := &fakeExec{
		running: map[string]bool{"goforge-service-pg1": true, "goforge-service-gt1": true, "goforge-service-vw1": true},
		outputs: map[string]string{"SELECT datname": "app\npostgres\nmy db\n", "pg_dump -U": "PGDMP", "tar -C /data": "tgz", "--warning=no-file-changed": "tgz"},
		exit:    map[string]int{"--warning=no-file-changed": 1},
	}
	src := &InstanceSource{Docker: ex, Instances: fakeLister{
		{ID: "pg1", TemplateName: "postgres", Name: "shared-pg"},
		{ID: "pg2", TemplateName: "postgres", Name: "stopped-pg"},
		{ID: "gt1", TemplateName: "gitea", Name: "git"},
		{ID: "vw1", TemplateName: "vaultwarden", Name: "vw"},
		{ID: "rd1", TemplateName: "redis", Name: "shared-redis"},
	}}
	targets, skipped, err := src.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 3 || len(skipped) != 2 {
		t.Fatalf("targets=%d skipped=%+v", len(targets), skipped)
	}
	if skipped[0].InstanceID != "pg2" || skipped[0].Reason != "container is not running" || skipped[1].Template != "redis" {
		t.Errorf("skipped = %+v", skipped)
	}

	pgArts, err := targets[0].Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(pgArts))
	for _, a := range pgArts {
		paths = append(paths, a.Path)
	}
	if got := strings.Join(paths, ","); got != "services/pg1/app.dump,services/pg1/postgres.dump,services/pg1/my_db.dump,services/pg1/roles.sql" {
		t.Errorf("postgres artifacts = %s", got)
	}
	var buf bytes.Buffer
	if err := pgArts[2].Dump(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	last := ex.calls[len(ex.calls)-1]
	if last.Cmd[len(last.Cmd)-1] != "my db" || pgArts[2].Database != "my db" {
		t.Errorf("db name must be passed as an argument, not spliced into the script: %q", last.Cmd)
	}

	giteaArts, _ := targets[1].Plan(context.Background())
	if len(giteaArts) != 2 || giteaArts[0].Kind != KindSQLite || giteaArts[1].Kind != KindTarGz {
		t.Errorf("gitea artifacts = %+v", giteaArts)
	}
	if err := giteaArts[0].Dump(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if ex.calls[len(ex.calls)-1].User != "git" {
		t.Error("gitea sqlite snapshot must run as git")
	}

	vwArts, _ := targets[2].Plan(context.Background())
	if err := vwArts[1].Dump(context.Background(), io.Discard); err != nil {
		t.Errorf("vaultwarden tar exit 1 handling is in the script; exec saw: %v", err)
	}
}

func TestInstanceTarget_NonZeroExitIsRedactedError(t *testing.T) {
	ex := &fakeExec{
		running: map[string]bool{"goforge-service-gt1": true},
		outputs: map[string]string{"sqlite3": "partial"},
		exit:    map[string]int{"sqlite3": 3},
	}
	src := &InstanceSource{Docker: ex, Instances: fakeLister{{ID: "gt1", TemplateName: "gitea", Name: "git"}}}
	targets, _, _ := src.Targets(context.Background())
	arts, _ := targets[0].Plan(context.Background())
	err := arts[0].Dump(context.Background(), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "exit code 3") || strings.Contains(err.Error(), ":b@") {
		t.Errorf("err = %v", err)
	}
	var target interface{ Name() string } = targets[0]
	if !strings.Contains(target.Name(), "gitea:git") {
		t.Errorf("name = %q", target.Name())
	}
}
