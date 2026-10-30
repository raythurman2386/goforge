package backup

import (
	"context"
	"errors"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// scheduleWait is how long the scheduler sleeps before the run at next.
// Tests shorten it.
var scheduleWait = time.Until

// Schedule runs the runner at every scheduled time until ctx is done. A run
// missed while GoForge was down is not caught up; the next run is the next
// scheduled time.
func (r *Runner) Schedule(ctx context.Context, sched config.DailySchedule) {
	r.ScheduleLoop(ctx, ctx, sched)
}

// ScheduleLoop is Schedule with separate lifetimes. The loop stops when
// loopCtx is done (Settings saved new backup settings). A run that already
// started uses runCtx instead, so it finishes with the settings it started
// with, holding the advisory lock; runCtx ends only at shutdown.
func (r *Runner) ScheduleLoop(loopCtx, runCtx context.Context, sched config.DailySchedule) {
	log := r.logger()
	if err := r.Recover(loopCtx); err != nil && !errors.Is(err, repositories.ErrBackupLocked) && loopCtx.Err() == nil {
		log.Warn("backup recovery check failed", "error", r.Redactor.Redact(err.Error()))
	}
	for {
		next := sched.Next(r.now())
		log.Info("next backup scheduled", "at", next.Format(time.RFC3339), "schedule", sched.String())
		timer := time.NewTimer(scheduleWait(next))
		select {
		case <-loopCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if _, err := r.Run(runCtx, models.BackupTriggerSchedule); err != nil {
			log.Error("scheduled backup did not start", "error", r.Redactor.Redact(err.Error()))
		}
		if loopCtx.Err() != nil {
			return // settings changed during the run; the new loop takes over
		}
	}
}
