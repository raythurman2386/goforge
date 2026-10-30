package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testPrefix = "goforge/"

func putRun(t *testing.T, s *memStore, runKey string, version int, dump []byte) {
	t.Helper()
	sum := sha256.Sum256(dump)
	m := &Manifest{
		Version: version, RunKey: runKey, RunID: "r1", Trigger: "manual", GoForgeVersion: "0.1.0",
		StartedAt: time.Date(2026, 10, 29, 7, 0, 0, 0, time.UTC),
		Artifacts: []ManifestArtifact{
			{Key: AppDumpKey, Target: "app", Kind: KindPgDump, Bytes: int64(len(dump)), SHA256: hex.EncodeToString(sum[:])},
			{Key: "services/x/db.dump", Target: "x", Kind: KindPgDump, Bytes: 3, SHA256: "abc"},
		},
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Put(ctx, ObjectKey(testPrefix, runKey, AppDumpKey), bytes.NewReader(dump), ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, ObjectKey(testPrefix, runKey, ManifestName), bytes.NewReader(body), ""); err != nil {
		t.Fatal(err)
	}
}

func mustPut(t *testing.T, s *memStore, key, body string) {
	t.Helper()
	if err := s.Put(context.Background(), key, strings.NewReader(body), ""); err != nil {
		t.Fatal(err)
	}
}

func TestValidRunKey(t *testing.T) {
	for k, want := range map[string]bool{
		"2026-10-29/20261029T070000Z":    true,
		"2026-10-29/20261029T070000z":    false,
		"2026-10-29/20261029T070000Z/":   false,
		"../2026-10-29/20261029T070000Z": false,
		"2026-10-29":                     false,
		"":                               false,
	} {
		if got := ValidRunKey(k); got != want {
			t.Errorf("ValidRunKey(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestReadManifest_Refusals(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	const key = "2026-10-29/20261029T070000Z"

	if _, err := ReadManifest(ctx, s, testPrefix, "nope"); !errors.Is(err, ErrBadRunKey) {
		t.Fatalf("bad key: %v", err)
	}
	// Dump without a manifest: incomplete run.
	if err := s.Put(ctx, ObjectKey(testPrefix, key, AppDumpKey), strings.NewReader("x"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(ctx, s, testPrefix, key); !errors.Is(err, ErrRunIncomplete) {
		t.Fatalf("no manifest: %v", err)
	}
	putRun(t, s, key, ManifestVersion+1, []byte("dump"))
	if _, err := ReadManifest(ctx, s, testPrefix, key); !errors.Is(err, ErrManifestTooNew) {
		t.Fatalf("newer manifest: %v", err)
	}
	putRun(t, s, key, ManifestVersion, []byte("dump"))
	m, err := ReadManifest(ctx, s, testPrefix, key)
	if err != nil {
		t.Fatal(err)
	}
	if a := m.Artifact(AppDumpKey); a == nil || a.Bytes != 4 {
		t.Fatalf("artifact = %+v", a)
	}
	if m.TotalBytes() != 7 {
		t.Fatalf("TotalBytes = %d", m.TotalBytes())
	}
}

func TestParseManifest_RunKeyMismatchAndVersion(t *testing.T) {
	if _, err := ParseManifest([]byte(`{"version":1,"run_key":"2026-10-28/20261028T070000Z"}`), "2026-10-29/20261029T070000Z"); err == nil {
		t.Fatal("run_key mismatch accepted")
	}
	if _, err := ParseManifest([]byte(`{"version":0,"run_key":"2026-10-29/20261029T070000Z"}`), "2026-10-29/20261029T070000Z"); err == nil {
		t.Fatal("version 0 accepted")
	}
	if _, err := ParseManifest([]byte(`not json`), "2026-10-29/20261029T070000Z"); err == nil {
		t.Fatal("bad json accepted")
	}
}

func TestRestorePoints_NewestFirstAndIncomplete(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	putRun(t, s, "2026-10-28/20261028T070000Z", 1, []byte("a"))
	putRun(t, s, "2026-10-29/20261029T070000Z", 1, []byte("b"))
	putRun(t, s, "2026-10-29/20261029T180000Z", 2, []byte("c")) // newer manifest version: listed with a problem
	// Incomplete run and a stray key.
	mustPut(t, s, ObjectKey(testPrefix, "2026-10-30/20261030T070000Z", AppDumpKey), "x")
	mustPut(t, s, testPrefix+"notes.txt", "x")
	// Another prefix is never read.
	mustPut(t, s, "other/2026-10-30/20261030T090000Z/manifest.json", "{}")

	points, incomplete, err := RestorePoints(ctx, s, testPrefix, 0)
	if err != nil {
		t.Fatal(err)
	}
	if incomplete != 1 {
		t.Errorf("incomplete = %d, want 1", incomplete)
	}
	keys := make([]string, 0, len(points))
	for _, p := range points {
		keys = append(keys, p.RunKey)
	}
	want := "2026-10-29/20261029T180000Z,2026-10-29/20261029T070000Z,2026-10-28/20261028T070000Z"
	if got := strings.Join(keys, ","); got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}
	if points[0].Manifest != nil || !strings.Contains(points[0].Problem, "newer") {
		t.Errorf("newer manifest point = %+v", points[0])
	}
	if points[1].Manifest == nil || points[1].Manifest.Trigger != "manual" {
		t.Errorf("point 1 = %+v", points[1])
	}
	limited, _, err := RestorePoints(ctx, s, testPrefix, 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limit: %d %v", len(limited), err)
	}
}

func TestRestorerPlan(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	const key = "2026-10-29/20261029T070000Z"
	putRun(t, s, key, 1, []byte("dump"))
	r := &Restorer{Store: s, Prefix: testPrefix, DatabaseURL: "postgres://u:pw-secret-123@db:5432/goforge?sslmode=disable"}
	plan, err := r.Plan(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Database != "goforge" || plan.SideDB != "goforge_restore_20261029t070000z" {
		t.Errorf("plan = %+v", plan)
	}
	if plan.ObjectKey != testPrefix+key+"/"+AppDumpKey {
		t.Errorf("object key = %s", plan.ObjectKey)
	}
	if _, err := r.Plan(ctx, "2026-10-30/20261030T070000Z"); !errors.Is(err, ErrRunIncomplete) {
		t.Errorf("missing run: %v", err)
	}
	for _, u := range []string{"postgres://u@db/postgres", "postgres://u@db/template1", "postgres://u@db/", "mysql://u@db/goforge"} {
		r.DatabaseURL = u
		if _, err := r.Plan(ctx, key); err == nil {
			t.Errorf("Plan accepted DATABASE_URL %q", u)
		}
	}
}

func TestRestorerDownload_Integrity(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()
	const key = "2026-10-29/20261029T070000Z"
	putRun(t, s, key, 1, []byte("the real dump"))
	dir := t.TempDir()
	r := &Restorer{Store: s, Prefix: testPrefix, DatabaseURL: "postgres://u@db/goforge", TempDir: dir}
	plan, err := r.Plan(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	path, err := r.download(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("temp file mode = %v, %v", fi.Mode(), err)
	}
	os.Remove(path)

	// Same size, different content: SHA-256 mismatch.
	mustPut(t, s, plan.ObjectKey, "the fake dump")
	if _, err := r.download(ctx, plan); !errors.Is(err, ErrRestoreIntegrity) || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("sha mismatch: %v", err)
	}
	// Different size.
	mustPut(t, s, plan.ObjectKey, "short")
	if _, err := r.download(ctx, plan); !errors.Is(err, ErrRestoreIntegrity) || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("size mismatch: %v", err)
	}
	// Missing object.
	if err := s.Delete(ctx, []string{plan.ObjectKey}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.download(ctx, plan); !errors.Is(err, ErrRestoreIntegrity) {
		t.Fatalf("missing dump: %v", err)
	}
	// Refused downloads leave no temp file behind.
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestFilterRestoreSQL(t *testing.T) {
	in := strings.Join([]string{
		"SET statement_timeout = 0;",
		"SET transaction_timeout = 0;",
		"SET lock_timeout = 0;",
		"COPY public.notes (id, body) FROM stdin;",
		"1\tSET transaction_timeout = 0;",
		"SET transaction_timeout = 0;",
		`\.`,
		"SET transaction_timeout = 0;",
		"SELECT 1;",
		"no trailing newline",
	}, "\n")
	var out bytes.Buffer
	if err := filterRestoreSQL(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"SET statement_timeout = 0;",
		"SET lock_timeout = 0;",
		"COPY public.notes (id, body) FROM stdin;",
		"1\tSET transaction_timeout = 0;",
		"SET transaction_timeout = 0;", // COPY data is never changed
		`\.`,
		"SELECT 1;",
		"no trailing newline",
	}, "\n")
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestDSNFor(t *testing.T) {
	got, err := dsnFor("postgres://u:p@db:5432/goforge?sslmode=require", "goforge_restore_x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/goforge_restore_x?") || !strings.Contains(got, "sslmode=require") || !strings.Contains(got, "application_name=goforge-restore") {
		t.Errorf("dsn = %s", got)
	}
	got, err = dsnFor("postgres://u:p@db/goforge", "postgres")
	if err != nil || !strings.Contains(got, "sslmode=disable") {
		t.Errorf("default sslmode: %s %v", got, err)
	}
	if _, err := dsnFor("://bad", "x"); err == nil || strings.Contains(err.Error(), "bad") {
		t.Errorf("bad url error leaks or is nil: %v", err)
	}
}
