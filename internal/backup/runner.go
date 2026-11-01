package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
)

// RunRecorder is the backup_runs storage the runner needs.
type RunRecorder interface {
	Lock(ctx context.Context) (unlock func(), err error)
	MarkInterrupted(ctx context.Context) (int64, error)
	Start(ctx context.Context, run *models.BackupRun) error
	Finish(ctx context.Context, run *models.BackupRun) error
}

// Runner performs backup runs.
type Runner struct {
	Store   Store
	Runs    RunRecorder
	Sources []Source
	// Prefix is the normalized key prefix ("" or "goforge/").
	Prefix    string
	Retention int
	// Location picks the date folder; normally the schedule's zone.
	Location *time.Location
	Timeout  time.Duration
	Version  string
	Redactor *Redactor
	Logger   *slog.Logger
	Now      func() time.Time
	// Notifier, when set, gets a backup.failed alert for every failed run.
	Notifier notify.Notifier
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Recover marks rows left running by a process that died mid-run. It is a
// no-op while another run holds the lock.
func (r *Runner) Recover(ctx context.Context) error {
	unlock, err := r.Runs.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	n, err := r.Runs.MarkInterrupted(ctx)
	if n > 0 {
		r.logger().Warn("marked interrupted backup runs as failed", "count", n)
	}
	return err
}

// Run performs one backup run and returns its finished row. A run that
// starts but fails is returned with status failed and a nil error; the error
// return is for runs that could not start (lock held, DB down).
func (r *Runner) Run(ctx context.Context, trigger models.BackupTrigger) (*models.BackupRun, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	unlock, err := r.Runs.Lock(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err = r.Runs.MarkInterrupted(ctx); err != nil {
		return nil, err
	}

	started := r.now()
	run := &models.BackupRun{Trigger: trigger, RunKey: RunKey(started, r.Location), StartedAt: started}
	if err := r.Runs.Start(ctx, run); err != nil {
		return nil, err
	}
	log := r.logger().With("backup_run", run.ID, "run_key", run.RunKey, "trigger", trigger)
	log.Info("backup run started")

	manifest := Manifest{
		Version: ManifestVersion, RunID: run.ID, RunKey: run.RunKey,
		Date: run.RunKey[:len(dateLayout)], Trigger: string(trigger),
		GoForgeVersion: r.Version, StartedAt: started.UTC(),
		Artifacts: []ManifestArtifact{}, Skipped: []Skipped{},
	}
	var failures []string
	fail := func(what string, err error) {
		msg := r.Redactor.Redact(what + ": " + err.Error())
		failures = append(failures, msg)
		log.Error("backup step failed", "step", what, "error", msg)
	}

	for _, src := range r.Sources {
		targets, skipped, serr := src.Targets(ctx)
		manifest.Skipped = append(manifest.Skipped, skipped...)
		for _, s := range skipped {
			log.Info("backup skipped instance", "instance_id", s.InstanceID, "template", s.Template, "reason", s.Reason)
		}
		if serr != nil {
			fail("enumerate targets", serr)
			continue
		}
		for _, t := range targets {
			arts, perr := t.Plan(ctx)
			if perr != nil {
				fail(t.Name(), perr)
				continue
			}
			for _, a := range arts {
				ma, uerr := r.upload(ctx, run.RunKey, t.Name(), a)
				if uerr != nil {
					fail(t.Name()+" "+a.Path, uerr)
					continue
				}
				manifest.Artifacts = append(manifest.Artifacts, ma)
				run.Objects++
				run.Bytes += ma.Bytes
				log.Info("backup artifact uploaded", "key", ma.Key, "bytes", ma.Bytes)
			}
		}
	}
	run.Skipped = len(manifest.Skipped)

	if len(failures) == 0 {
		manifest.FinishedAt = r.now().UTC()
		if merr := r.putManifest(ctx, run.RunKey, &manifest); merr != nil {
			fail("manifest", merr)
		} else {
			run.Objects++
		}
	}

	if len(failures) == 0 {
		run.Status = models.BackupRunSucceeded
		pruned, perr := r.prune(ctx)
		run.Pruned = pruned
		if perr != nil {
			// The backup itself is complete and usable; say so, and record why
			// old runs were not cleaned up.
			msg := r.Redactor.Redact("retention: " + perr.Error())
			run.ErrorSummary = sql.NullString{String: msg, Valid: true}
			log.Warn("backup retention failed", "error", msg)
		}
	} else {
		run.Status = models.BackupRunFailed
		run.ErrorSummary = sql.NullString{String: summarize(failures), Valid: true}
	}

	run.FinishedAt = sql.NullTime{Time: r.now(), Valid: true}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if ferr := r.Runs.Finish(finishCtx, run); ferr != nil {
		log.Error("failed to record backup run", "error", r.Redactor.Redact(ferr.Error()))
	}
	log.Info("backup run finished", "status", run.Status, "objects", run.Objects, "bytes", run.Bytes,
		"skipped", run.Skipped, "pruned", run.Pruned)
	r.notifyFailed(finishCtx, run)
	return run, nil
}

// notifyFailed sends a backup.failed alert when the run failed.
func (r *Runner) notifyFailed(ctx context.Context, run *models.BackupRun) {
	if run.Status != models.BackupRunFailed || r.Notifier == nil {
		return
	}
	r.Notifier.Notify(ctx, notify.Event{
		Kind:     notify.EventBackupFailed,
		Resource: notify.Resource{Type: models.ResourceTypeBackupRun, ID: run.ID, Name: run.RunKey},
		Error:    run.ErrorSummary.String,
		Path:     "/settings/profile#backup-status",
		Time:     r.now(),
	})
}

func summarize(failures []string) string {
	s := fmt.Sprintf("%d step(s) failed: %s", len(failures), strings.Join(failures, "; "))
	if len(s) > maxSummaryLen {
		s = s[:maxSummaryLen] + "…"
	}
	return s
}

// upload streams one artifact into the bucket while hashing it. If the dump
// fails, the pipe is closed with the error, which aborts the multipart
// upload, so a partial object is never left behind as if it were whole.
func (r *Runner) upload(ctx context.Context, runKey, target string, a Artifact) (ManifestArtifact, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	h := sha256.New()
	cw := &byteCounter{}
	dumpErr := make(chan error, 1)
	go func() {
		err := a.Dump(ctx, io.MultiWriter(pw, h, cw))
		if err == nil {
			err = io.EOF
		}
		pw.CloseWithError(err)
		if errors.Is(err, io.EOF) {
			err = nil
		}
		dumpErr <- err
	}()
	putErr := r.Store.Put(ctx, ObjectKey(r.Prefix, runKey, a.Path), pr, a.ContentType)
	if putErr != nil {
		cancel()
		pr.CloseWithError(putErr)
	}
	derr := <-dumpErr
	if putErr != nil {
		return ManifestArtifact{}, putErr
	}
	if derr != nil {
		return ManifestArtifact{}, derr
	}
	return ManifestArtifact{
		Key: a.Path, Target: target, Kind: a.Kind, Bytes: cw.n,
		SHA256: hex.EncodeToString(h.Sum(nil)), InstanceID: a.InstanceID,
		Template: a.Template, Database: a.Database,
	}, nil
}

func (r *Runner) putManifest(ctx context.Context, runKey string, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return r.Store.Put(ctx, ObjectKey(r.Prefix, runKey, ManifestName), bytes.NewReader(b), "application/json")
}

func (r *Runner) prune(ctx context.Context) (int, error) {
	objs, err := r.Store.List(ctx, r.Prefix)
	if err != nil {
		return 0, err
	}
	keys := make([]string, len(objs))
	for i, o := range objs {
		keys[i] = o.Key
	}
	del := PruneKeys(r.Prefix, keys, r.Retention)
	if len(del) == 0 {
		return 0, nil
	}
	if err := r.Store.Delete(ctx, del); err != nil {
		return 0, err
	}
	r.logger().Info("backup retention pruned objects", "count", len(del))
	return len(del), nil
}

type byteCounter struct{ n int64 }

func (c *byteCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
