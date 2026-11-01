package backup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/config"
)

type failingStore struct{ *memStore }

func (*failingStore) List(context.Context, string) ([]Object, error) {
	return nil, errors.New("list failed for key AKIA-view-secret-123 on http://u:pw@host")
}

func TestRestorePointLister(t *testing.T) {
	cfg := config.BackupConfig{Bucket: "bk", Prefix: testPrefix, AccessKeyID: "AKIA-view-secret-123", Schedule: "02:00 UTC"}
	s := newMemStore()
	putRun(t, s, "2026-10-29/20261029T070000Z", 1, []byte("dump"))
	l := &RestorePointLister{
		Current:  func() (config.BackupConfig, string) { return cfg, SourceSettings },
		Redactor: func(extra ...config.BackupConfig) *Redactor { return NewRedactor(extra[0].AccessKeyID) },
		NewStore: func(config.BackupConfig) (Store, error) { return s, nil },
	}
	v := l.List(context.Background())
	if v.Error != "" || len(v.Points) != 1 || v.Bucket != "bk" || v.Schedule != "02:00 UTC" {
		t.Fatalf("view = %+v", v)
	}

	l.NewStore = func(config.BackupConfig) (Store, error) { return &failingStore{newMemStore()}, nil }
	v = l.List(context.Background())
	if !strings.HasPrefix(v.Error, "Could not list the bucket") || strings.Contains(v.Error, "AKIA-view-secret-123") || strings.Contains(v.Error, ":pw@") {
		t.Fatalf("error = %q", v.Error)
	}

	l.Current = func() (config.BackupConfig, string) { return config.BackupConfig{Disabled: true}, SourceSettings }
	if v = l.List(context.Background()); v.Error != "No backup bucket is configured." {
		t.Fatalf("no bucket: %+v", v)
	}
}

func TestRestorePointLister_ListInstance(t *testing.T) {
	cfg := config.BackupConfig{Bucket: "bk", Prefix: testPrefix, Schedule: "02:00 America/Chicago"}
	s := newMemStore()
	putServiceRun(t, s)                                         // g1, v1, p1; "off" skipped
	putRun(t, s, "2026-10-31/20261031T070000Z", 1, []byte("d")) // app only
	l := &RestorePointLister{
		Current:  func() (config.BackupConfig, string) { return cfg, SourceSettings },
		NewStore: func(config.BackupConfig) (Store, error) { return s, nil },
	}
	v := l.ListInstance(context.Background(), "g1")
	if v.Error != "" || len(v.Points) != 1 || v.Template != templateGitea || !v.Restorable() || v.Schedule != cfg.Schedule {
		t.Fatalf("g1 view = %+v", v)
	}
	p := v.Points[0]
	if p.RunKey != svcRunKey || p.Artifacts != 2 || p.Bytes != int64(len("gitea-db")+len("gitea-tar")) || p.StartedAt.IsZero() {
		t.Errorf("g1 point = %+v", p)
	}
	if v := l.ListInstance(context.Background(), "p1"); len(v.Points) != 1 || !v.Restorable() || v.KeptDir() != "/var/lib/postgresql/data/.goforge-pre-restore-<time>" {
		t.Errorf("postgres view = %+v", v)
	}
	if v := l.ListInstance(context.Background(), "x1"); len(v.Points) != 1 || v.Restorable() {
		t.Errorf("redis view = %+v", v)
	}
	if v := l.ListInstance(context.Background(), "off"); len(v.Points) != 0 || v.Skipped != 1 {
		t.Errorf("skipped view = %+v", v)
	}
	if v := l.ListInstance(context.Background(), "other"); len(v.Points) != 0 || v.Error != "" || v.Template != "" {
		t.Errorf("unknown view = %+v", v)
	}
	l.Current = func() (config.BackupConfig, string) { return config.BackupConfig{}, SourceSettings }
	if v := l.ListInstance(context.Background(), "g1"); v.Error != "No backup bucket is configured." {
		t.Errorf("no bucket: %+v", v)
	}
}
