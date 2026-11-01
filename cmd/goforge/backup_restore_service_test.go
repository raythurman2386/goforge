package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/backup"
	"github.com/raythurman2386/goforge/internal/docker"
)

const svcKey = "2026-10-30/20261030T070000Z"

type mapStore map[string][]byte

func (m mapStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, err := io.ReadAll(r)
	m[key] = b
	return err
}
func (m mapStore) List(_ context.Context, prefix string) ([]backup.Object, error) {
	var out []backup.Object
	for k, v := range m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, backup.Object{Key: k, Size: int64(len(v))})
		}
	}
	return out, nil
}
func (m mapStore) Delete(context.Context, []string) error { return errors.New("read only") }
func (m mapStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := m[key]
	if !ok {
		return nil, backup.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func serviceRunStore(t *testing.T) mapStore {
	t.Helper()
	s := mapStore{}
	m := backup.Manifest{Version: 1, RunKey: svcKey, Trigger: "scheduled", GoForgeVersion: "0.1.0", StartedAt: time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)}
	for key, body := range map[string]string{"services/v1/vaultwarden.sqlite3": "db", "services/v1/data.tar.gz": "tar"} {
		sum := sha256.Sum256([]byte(body))
		kind := backup.KindTarGz
		if strings.HasSuffix(key, ".sqlite3") {
			kind = backup.KindSQLite
		}
		m.Artifacts = append(m.Artifacts, backup.ManifestArtifact{Key: key, Kind: kind, InstanceID: "v1", Template: "vaultwarden", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])})
		s["goforge/"+svcKey+"/"+key] = []byte(body)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s["goforge/"+svcKey+"/manifest.json"] = body
	return s
}

type cliFakeDocker struct{ calls []string }

func (f *cliFakeDocker) Inspect(context.Context, string) (*backup.ServiceContainerInfo, error) {
	return &backup.ServiceContainerInfo{ID: "cid", Image: "img", Running: true}, nil
}
func (f *cliFakeDocker) Stop(context.Context, string) error {
	f.calls = append(f.calls, "stop")
	return nil
}
func (f *cliFakeDocker) Start(context.Context, string) error {
	f.calls = append(f.calls, "start")
	return nil
}
func (f *cliFakeDocker) WaitHealthy(context.Context, string) error {
	f.calls = append(f.calls, "healthy")
	return nil
}
func (f *cliFakeDocker) StartHelper(context.Context, docker.HelperOptions) (string, error) {
	f.calls = append(f.calls, "helper")
	return "h", nil
}
func (f *cliFakeDocker) RemoveHelper(context.Context, string) error {
	f.calls = append(f.calls, "rmhelper")
	return nil
}
func (f *cliFakeDocker) CopyToContainer(_ context.Context, _, _ string, r io.Reader) error {
	_, err := io.Copy(io.Discard, r)
	return err
}
func (f *cliFakeDocker) ExecStream(context.Context, string, docker.ExecOptions, io.Writer) (docker.ExecStreamResult, error) {
	f.calls = append(f.calls, "exec")
	return docker.ExecStreamResult{}, nil
}

func newCLIServiceRestorer(t *testing.T, d *cliFakeDocker, out io.Writer) *backup.ServiceRestorer {
	return &backup.ServiceRestorer{Store: serviceRunStore(t), Prefix: "goforge/", Docker: d, TempDir: t.TempDir(), Progress: out}
}

func TestRestoreService_DryRunChangesNothing(t *testing.T) {
	var out bytes.Buffer
	d := &cliFakeDocker{}
	if err := restoreService(context.Background(), newCLIServiceRestorer(t, d, &out), svcKey, "v1", serviceRestoreOpts{}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(d.calls) != 0 {
		t.Errorf("dry run touched docker: %v", d.calls)
	}
	for _, want := range []string{"Service restore plan", "v1 (vaultwarden), container goforge-service-v1", "services/v1/data.tar.gz, 3 bytes, sha256 ",
		".goforge-pre-restore-", "preflight:  ok", "Dry run: nothing changed", "goforge backup restore-service " + svcKey + " v1 --yes"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestRestoreService_ConfirmMismatchRefusesBeforeStop(t *testing.T) {
	var out bytes.Buffer
	d := &cliFakeDocker{}
	err := restoreService(context.Background(), newCLIServiceRestorer(t, d, &out), svcKey, "v1",
		serviceRestoreOpts{Yes: true, Confirm: "2026-10-29/20261029T070000Z"}, strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "does not match") || len(d.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, d.calls)
	}
	// No terminal and no --confirm: refused too.
	err = restoreService(context.Background(), newCLIServiceRestorer(t, d, &out), svcKey, "v1",
		serviceRestoreOpts{Yes: true}, strings.NewReader(svcKey+"\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "--confirm") || len(d.calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, d.calls)
	}
}

func TestRestoreService_YesRuns(t *testing.T) {
	var out bytes.Buffer
	d := &cliFakeDocker{}
	if err := restoreService(context.Background(), newCLIServiceRestorer(t, d, &out), svcKey, "v1",
		serviceRestoreOpts{Yes: true, Confirm: svcKey}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := strings.Join(d.calls, ","); got != "stop,helper,exec,rmhelper,start,healthy" {
		t.Errorf("calls = %s", got)
	}
	if !strings.Contains(out.String(), "running and healthy") || !strings.Contains(out.String(), "/data/.goforge-pre-restore-") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestCheckServiceRestoreArgs(t *testing.T) {
	if err := checkServiceRestoreArgs("../x", "v1", false, ""); !errors.Is(err, backup.ErrBadRunKey) {
		t.Errorf("bad key: %v", err)
	}
	if err := checkServiceRestoreArgs(svcKey, "../v1", false, ""); !errors.Is(err, backup.ErrBadInstanceID) {
		t.Errorf("bad id: %v", err)
	}
	if err := checkServiceRestoreArgs(svcKey, "v1", false, svcKey); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("confirm without yes: %v", err)
	}
	if err := checkServiceRestoreArgs(svcKey, "550e8400-e29b-41d4-a716-446655440000", true, svcKey); err != nil {
		t.Errorf("valid: %v", err)
	}
}

func postgresRunStore(t *testing.T) mapStore {
	t.Helper()
	s := mapStore{}
	m := backup.Manifest{Version: 1, RunKey: svcKey, Trigger: "scheduled", GoForgeVersion: "0.1.0", StartedAt: time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)}
	for _, a := range []struct{ key, body, db string }{{"services/p1/app.dump", "dump", "app"}, {"services/p1/roles.sql", "roles", ""}} {
		sum := sha256.Sum256([]byte(a.body))
		kind := backup.KindPgDump
		if a.db == "" {
			kind = backup.KindPgRoles
		}
		m.Artifacts = append(m.Artifacts, backup.ManifestArtifact{Key: a.key, Kind: kind, InstanceID: "p1", Template: "postgres", Database: a.db, Bytes: int64(len(a.body)), SHA256: hex.EncodeToString(sum[:])})
		s["goforge/"+svcKey+"/"+a.key] = []byte(a.body)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s["goforge/"+svcKey+"/manifest.json"] = body
	return s
}

func TestRestoreService_PostgresDryRunAndYes(t *testing.T) {
	var out bytes.Buffer
	d := &cliFakeDocker{}
	r := &backup.ServiceRestorer{Store: postgresRunStore(t), Prefix: "goforge/", Docker: d, TempDir: t.TempDir(), Progress: &out}
	if err := restoreService(context.Background(), r, svcKey, "p1", serviceRestoreOpts{}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(d.calls) != 0 {
		t.Errorf("dry run touched docker: %v", d.calls)
	}
	for _, want := range []string{"p1 (postgres), container goforge-service-p1", "roles:      services/p1/roles.sql, 5 bytes",
		"database:   app (services/p1/app.dump), 4 bytes", "superuser:  postgres (POSTGRES_USER)",
		"/var/lib/postgresql/data/.goforge-pre-restore-<UTC time>/ (kept)", "preflight:  ok"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := restoreService(context.Background(), r, svcKey, "p1", serviceRestoreOpts{Yes: true, Confirm: svcKey}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := strings.Join(d.calls, ","); got != "stop,helper,exec,rmhelper,start,healthy" {
		t.Errorf("calls = %s", got)
	}
	for _, want := range []string{"Restored p1 (postgres)", "Rows written after the backup are gone",
		"back to /var/lib/postgresql/data."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}
