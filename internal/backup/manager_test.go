package backup

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
)

// blockingTarget dumps one artifact and waits for release while dumping.
type blockingTarget struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingTarget) Name() string { return "app-db" }
func (b *blockingTarget) Plan(context.Context) ([]Artifact, error) {
	return []Artifact{{Path: "app/goforge-db.dump", Kind: KindPgDump, Dump: func(ctx context.Context, w io.Writer) error {
		close(b.started)
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := w.Write([]byte("dump"))
		return err
	}}}, nil
}

// Saving new settings mid-run must not cancel the run: it finishes with the
// old config, and the old scheduler loop then stops.
func TestManager_ApplyKeepsInFlightRun(t *testing.T) {
	orig := scheduleWait
	scheduleWait = func(time.Time) time.Duration { return 0 }
	t.Cleanup(func() { scheduleWait = orig })

	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, runs := newMemStore(), &memRuns{}
	tgt := &blockingTarget{started: make(chan struct{}), release: make(chan struct{})}
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	sched, _, err := config.ParseDailySchedule("02:00 UTC")
	if err != nil {
		t.Fatal(err)
	}
	builds := 0
	m := &Manager{
		parent: parent,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		build: func(cfg config.BackupConfig) (*Setup, error) {
			builds++
			if !cfg.Enabled() {
				return nil, ErrDisabled
			}
			r := newTestRunner(store, runs, &now, Static{tgt})
			return &Setup{Runner: r, Schedule: sched, Scheduled: true}, nil
		},
	}
	on := envBackup()
	m.Apply(on, SourceSettings)

	select {
	case <-tgt.started:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled run did not start")
	}
	off := on
	off.Disabled = true
	m.Apply(off, SourceSettings) // cancels the loop, not the run
	if cfg, src := m.Current(); cfg.Enabled() || src != SourceSettings {
		t.Fatalf("Current = %+v %s", cfg, src)
	}
	close(tgt.release)

	done := make(chan struct{})
	go func() { m.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("old scheduler loop did not stop after its run")
	}
	if len(runs.finished) != 1 || runs.finished[0].Status != models.BackupRunSucceeded {
		t.Fatalf("finished = %+v", runs.finished)
	}
	if builds != 2 {
		t.Errorf("builds = %d", builds)
	}
}

// Apply after shutdown starts nothing.
func TestManager_ApplyAfterShutdown(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	m := &Manager{parent: parent, build: func(config.BackupConfig) (*Setup, error) {
		t.Fatal("build after shutdown")
		return nil, nil
	}}
	m.Apply(envBackup(), SourceEnvironment)
	m.Wait()
}
