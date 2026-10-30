package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

type memStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	failPut string
}

func newMemStore() *memStore { return &memStore{objects: map[string][]byte{}} }

func (m *memStore) Put(_ context.Context, key string, r io.Reader, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if m.failPut != "" && strings.Contains(key, m.failPut) {
		return errors.New("injected put failure")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = b
	return nil
}

func (m *memStore) List(_ context.Context, prefix string) ([]Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Object
	for k, v := range m.objects {
		if strings.HasPrefix(k, prefix) {
			out = append(out, Object{Key: k, Size: int64(len(v))})
		}
	}
	return out, nil
}

func (m *memStore) Delete(_ context.Context, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.objects, k)
	}
	return nil
}

func (m *memStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStore) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.objects))
	for k := range m.objects {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type memRuns struct {
	mu       sync.Mutex
	locked   bool
	runs     []*models.BackupRun
	finished []*models.BackupRun
}

func (m *memRuns) Lock(context.Context) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked {
		return nil, repositories.ErrBackupLocked
	}
	m.locked = true
	return func() { m.mu.Lock(); m.locked = false; m.mu.Unlock() }, nil
}

func (m *memRuns) MarkInterrupted(context.Context) (int64, error) { return 0, nil }

func (m *memRuns) Start(_ context.Context, r *models.BackupRun) error {
	r.ID = "run-" + r.RunKey
	r.Status = models.BackupRunRunning
	m.runs = append(m.runs, r)
	return nil
}

func (m *memRuns) Finish(_ context.Context, r *models.BackupRun) error {
	cp := *r
	m.finished = append(m.finished, &cp)
	return nil
}

type fakeTarget struct {
	name    string
	arts    []Artifact
	planErr error
}

func (f *fakeTarget) Name() string                             { return f.name }
func (f *fakeTarget) Plan(context.Context) ([]Artifact, error) { return f.arts, f.planErr }
func bytesArtifact(path string, data []byte) Artifact {
	return Artifact{Path: path, Kind: KindPgDump, Dump: func(_ context.Context, w io.Writer) error {
		_, err := w.Write(data)
		return err
	}}
}

type fakeSource struct {
	targets []Target
	skipped []Skipped
}

func (f fakeSource) Targets(context.Context) ([]Target, []Skipped, error) {
	return f.targets, f.skipped, nil
}

func newTestRunner(store Store, runs RunRecorder, now *time.Time, sources ...Source) *Runner {
	return &Runner{
		Store: store, Runs: runs, Sources: sources, Prefix: "goforge/", Retention: 2,
		Location: time.UTC, Version: "test", Redactor: NewRedactor("TOPSECRET-ENC-KEY"),
		Now: func() time.Time { return *now },
	}
}

func TestRunner_SuccessWritesManifestLastAndPrunes(t *testing.T) {
	store := newMemStore()
	runs := &memRuns{}
	now := time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC)
	big := bytes.Repeat([]byte("x"), 3<<20)
	src := fakeSource{
		targets: []Target{&fakeTarget{name: "app-db", arts: []Artifact{bytesArtifact("app/goforge-db.dump", big)}}},
		skipped: []Skipped{{InstanceID: "r1", Template: "redis", Reason: "template has no backup target in v1"}},
	}
	store.objects["goforge/unrelated.txt"] = []byte("keep me")
	r := newTestRunner(store, runs, &now, src)

	var last *models.BackupRun
	for range 4 {
		var err error
		last, err = r.Run(context.Background(), models.BackupTriggerManual)
		if err != nil {
			t.Fatal(err)
		}
		if last.Status != models.BackupRunSucceeded {
			t.Fatalf("status = %s (%s)", last.Status, last.ErrorSummary.String)
		}
		now = now.Add(24 * time.Hour)
	}
	if last.Objects != 2 || last.Bytes != int64(len(big)) || last.Skipped != 1 || last.Pruned != 2 {
		t.Errorf("last run = %+v", last)
	}

	want := []string{
		"goforge/2026-10-29/20261029T070000Z/app/goforge-db.dump",
		"goforge/2026-10-29/20261029T070000Z/manifest.json",
		"goforge/2026-10-30/20261030T070000Z/app/goforge-db.dump",
		"goforge/2026-10-30/20261030T070000Z/manifest.json",
		"goforge/unrelated.txt",
	}
	if got := store.keys(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("keys after 4 runs with retention 2:\n%s", strings.Join(got, "\n"))
	}

	var m Manifest
	if err := json.Unmarshal(store.objects["goforge/2026-10-30/20261030T070000Z/manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(big)
	if m.Version != ManifestVersion || m.RunID != last.ID || m.Date != "2026-10-30" || len(m.Artifacts) != 1 ||
		m.Artifacts[0].SHA256 != hex.EncodeToString(sum[:]) || m.Artifacts[0].Bytes != int64(len(big)) ||
		len(m.Skipped) != 1 || m.Trigger != "manual" {
		t.Errorf("manifest = %+v", m)
	}
}

func TestRunner_FailureWritesNoManifestAndNeverPrunes(t *testing.T) {
	store := newMemStore()
	runs := &memRuns{}
	now := time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC)
	// Two old complete days that retention 2 would otherwise trim after a
	// third successful day.
	for _, k := range []string{
		"goforge/2026-10-20/20261020T070000Z/manifest.json",
		"goforge/2026-10-21/20261021T070000Z/manifest.json",
		"goforge/2026-10-22/20261022T070000Z/manifest.json",
	} {
		store.objects[k] = []byte("{}")
	}
	failing := Artifact{Path: "services/x/data.tar.gz", Kind: KindTarGz, Dump: func(_ context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return errors.New("exit code 2: tar: boom TOPSECRET-ENC-KEY")
	}}
	src := fakeSource{targets: []Target{
		&fakeTarget{name: "app-db", arts: []Artifact{bytesArtifact("app/goforge-db.dump", []byte("ok"))}},
		&fakeTarget{name: "gitea:x", arts: []Artifact{failing}},
		&fakeTarget{name: "postgres:y", planErr: errors.New("list databases: no route")},
	}}
	r := newTestRunner(store, runs, &now, src)

	got, err := r.Run(context.Background(), models.BackupTriggerSchedule)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.BackupRunFailed || got.Pruned != 0 {
		t.Fatalf("run = %+v", got)
	}
	if strings.Contains(got.ErrorSummary.String, "TOPSECRET") || !strings.Contains(got.ErrorSummary.String, "2 step(s) failed") {
		t.Errorf("error summary = %q", got.ErrorSummary.String)
	}
	for _, k := range store.keys() {
		if strings.HasPrefix(k, "goforge/2026-10-27/") && strings.HasSuffix(k, ManifestName) {
			t.Errorf("failed run wrote a manifest: %s", k)
		}
		if strings.HasSuffix(k, "data.tar.gz") {
			t.Errorf("failed artifact left an object: %s", k)
		}
	}
	if len(store.keys()) != 4 { // 3 old manifests + the good app dump
		t.Errorf("keys = %v", store.keys())
	}
	if len(runs.finished) != 1 || runs.finished[0].Status != models.BackupRunFailed {
		t.Errorf("finished rows = %+v", runs.finished)
	}
}

func TestRunner_PutFailureFailsRun(t *testing.T) {
	store := newMemStore()
	store.failPut = "manifest.json"
	runs := &memRuns{}
	now := time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC)
	r := newTestRunner(store, runs, &now, Static{&fakeTarget{name: "app-db", arts: []Artifact{bytesArtifact("app/goforge-db.dump", []byte("ok"))}}})
	got, err := r.Run(context.Background(), models.BackupTriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.BackupRunFailed || !strings.Contains(got.ErrorSummary.String, "manifest") {
		t.Errorf("run = %+v", got)
	}
}

func TestRunner_LockHeld(t *testing.T) {
	runs := &memRuns{}
	unlock, _ := runs.Lock(context.Background())
	defer unlock()
	now := time.Now()
	r := newTestRunner(newMemStore(), runs, &now)
	if _, err := r.Run(context.Background(), models.BackupTriggerManual); !errors.Is(err, repositories.ErrBackupLocked) {
		t.Errorf("Run() err = %v, want ErrBackupLocked", err)
	}
	if len(runs.runs) != 0 {
		t.Error("a locked-out run must not insert a row")
	}
}
