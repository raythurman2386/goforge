package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/notify/notifytest"
)

// A failed run POSTs one backup.failed with the run, its redacted summary,
// and a link to the Backups card. A good run sends nothing.
func TestRunner_FailedRunNotifiesOnce(t *testing.T) {
	n, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, "/hook", "")
	now := time.Date(2026, 10, 27, 7, 0, 0, 0, time.UTC)

	good := newTestRunner(newMemStore(), &memRuns{}, &now, Static{&fakeTarget{name: "app-db", arts: []Artifact{bytesArtifact("app/ok.dump", []byte("ok"))}}})
	good.Notifier = n
	if run, err := good.Run(context.Background(), models.BackupTriggerSchedule); err != nil || run.Status != models.BackupRunSucceeded {
		t.Fatalf("good run: %+v %v", run, err)
	}

	failing := Artifact{Path: "services/x/data.tar.gz", Kind: KindTarGz, Dump: func(_ context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("partial"))
		return errors.New("exit code 2: tar: boom TOPSECRET-ENC-KEY")
	}}
	bad := newTestRunner(newMemStore(), &memRuns{}, &now, Static{&fakeTarget{name: "gitea:x", arts: []Artifact{failing}}})
	bad.Notifier = n
	run, err := bad.Run(context.Background(), models.BackupTriggerSchedule)
	if err != nil || run.Status != models.BackupRunFailed {
		t.Fatalf("bad run: %+v %v", run, err)
	}
	n.Wait()

	reqs := rcv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("POSTs = %d, want 1", len(reqs))
	}
	p := reqs[0].Payload(t)
	if p.Event != notify.EventBackupFailed || p.Resource.Type != models.ResourceTypeBackupRun || p.Resource.ID != run.ID || p.Resource.Name != run.RunKey {
		t.Errorf("payload %+v", p)
	}
	if !strings.Contains(p.Error, "1 step(s) failed") || p.Link != "https://forge.example.test/settings/profile#backup-status" {
		t.Errorf("error %q link %q", p.Error, p.Link)
	}
	if strings.Contains(reqs[0].Body, "TOPSECRET") {
		t.Errorf("payload leaks a secret: %s", reqs[0].Body)
	}
}

type recNotifier struct {
	events chan notify.Event
}

// Notify never blocks: with no schedule wait the loop runs back to back.
func (r *recNotifier) Notify(_ context.Context, ev notify.Event) {
	select {
	case r.events <- ev:
	default:
	}
}

// The Manager hands its notifier to every scheduled runner it builds.
func TestManager_ScheduledFailedRunNotifies(t *testing.T) {
	orig := scheduleWait
	scheduleWait = func(time.Time) time.Duration { return 0 }
	t.Cleanup(func() { scheduleWait = orig })

	parent, cancel := context.WithCancel(context.Background())
	sched, _, err := config.ParseDailySchedule("02:00 UTC")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 7, 0, 0, 0, time.UTC)
	rec := &recNotifier{events: make(chan notify.Event, 8)}
	m := (&Manager{
		parent: parent,
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		build: func(config.BackupConfig) (*Setup, error) {
			r := newTestRunner(newMemStore(), &memRuns{}, &now, Static{&fakeTarget{name: "postgres:y", planErr: errors.New("list databases: no route")}})
			return &Setup{Runner: r, Schedule: sched, Scheduled: true}, nil
		},
	}).WithNotifier(rec)
	m.Apply(envBackup(), SourceSettings)
	select {
	case ev := <-rec.events:
		if ev.Kind != notify.EventBackupFailed || !strings.Contains(ev.Error, "no route") {
			t.Errorf("event %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no backup.failed from the scheduled run")
	}
	cancel()
	m.Wait()
}
