package backup

import (
	"context"
	"strings"
	"testing"
)

func TestShippedTarScriptsStayPut(t *testing.T) {
	g := Spec{Strategy: strategySQLiteVolume, Mount: "/data", Snapshot: templateGitea, Excludes: append([]string(nil), snapshotBuiltins[templateGitea].excludes...)}
	if err := g.Normalize(); err != nil {
		t.Fatal(err)
	}
	if g.tarCommand() != giteaTar {
		t.Fatalf("gitea tar script changed:\n%s", g.tarCommand())
	}
	v := Spec{
		Strategy: strategySQLiteVolume, Mount: "/data", Snapshot: templateVaultwarden, ChangedOK: true,
		Excludes: append([]string(nil), snapshotBuiltins[templateVaultwarden].excludes...),
	}
	if err := v.Normalize(); err != nil {
		t.Fatal(err)
	}
	if v.tarCommand() != vaultwardenTar {
		t.Fatalf("vaultwarden tar script changed:\n%s", v.tarCommand())
	}
}

func TestSpec_RejectsBadPaths(t *testing.T) {
	bad := []Spec{
		{Strategy: strategyVolume, Mount: "data"},
		{Strategy: strategyVolume, Mount: "/data/../etc"},
		{Strategy: strategySQLiteVolume, Mount: "/data", Snapshot: templateGitea, Excludes: []string{"--checkpoint"}},
		{Strategy: strategySQLiteVolume, Mount: "/data", Snapshot: "redis"},
		{Strategy: "redis"},
	}
	for _, spec := range bad {
		if spec.Usable() {
			t.Errorf("usable: %+v", spec)
		}
	}
}

type specSource map[string]Spec

func (m specSource) BackupSpec(name string) (Spec, bool) {
	s, ok := m[name]
	return s, ok
}

func TestInstanceSource_VolumeAndSQLiteVolume(t *testing.T) {
	ex := &fakeExec{
		running: map[string]bool{"goforge-service-m": true, "goforge-service-g": true},
		rules:   []execRule{{match: "sqlite3", stdout: "db"}, {match: "tar -C /data", stdout: "tgz"}},
	}
	src := &InstanceSource{
		Docker: ex,
		Strategies: specSource{
			"minio": {Strategy: strategyVolume, Mount: "/data"},
			"git":   {Strategy: strategySQLiteVolume, Mount: "/data", Snapshot: templateGitea, Excludes: snapshotBuiltins[templateGitea].excludes},
		},
		Instances: fakeLister{
			{ID: "m", TemplateName: "minio", Name: "files", BackupEnabled: true},
			{ID: "g", TemplateName: "git", Name: "git", BackupEnabled: true},
		},
	}
	targets, skipped, err := src.Targets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 || len(targets) != 2 {
		t.Fatalf("targets=%d skipped=%+v", len(targets), skipped)
	}
	vol, err := targets[0].Plan(context.Background())
	if err != nil || len(vol) != 1 || vol[0].Kind != KindTarGz || vol[0].Mount != "/data" || vol[0].Strategy != strategyVolume {
		t.Fatalf("volume plan = %+v %v", vol, err)
	}
	sql, err := targets[1].Plan(context.Background())
	if err != nil || len(sql) != 2 || sql[0].Kind != KindSQLite || sql[0].Snapshot != templateGitea || sql[1].Kind != KindTarGz {
		t.Fatalf("sqlite plan = %+v %v", sql, err)
	}
	if err := sql[0].Dump(context.Background(), ioDiscard{}); err != nil {
		t.Fatal(err)
	}
	if ex.calls[len(ex.calls)-1].User != "git" {
		t.Fatal("gitea snapshot must run as git")
	}
	if !strings.Contains(strings.Join(ex.calls[len(ex.calls)-1].Cmd, "\n"), "/data") {
		t.Fatal("snapshot was not told the mount")
	}
}

// ioDiscard is an io.Writer that drops bytes. The test only cares about the exec options.
type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
