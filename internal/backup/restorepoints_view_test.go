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
