package backup

import (
	"context"
	"errors"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// Schedule runs the runner at every scheduled time until ctx is done. A run
// missed while GoForge was down is not caught up; the next run is the next
// scheduled time.
func (r *Runner) Schedule(ctx context.Context, sched config.DailySchedule) {
	log := r.logger()
	if err := r.Recover(ctx); err != nil && !errors.Is(err, repositories.ErrBackupLocked) {
		log.Warn("backup recovery check failed", "error", r.Redactor.Redact(err.Error()))
	}
	for {
		next := sched.Next(r.now())
		log.Info("next backup scheduled", "at", next.Format(time.RFC3339), "schedule", sched.String())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if _, err := r.Run(ctx, models.BackupTriggerSchedule); err != nil {
			log.Error("scheduled backup did not start", "error", r.Redactor.Redact(err.Error()))
		}
	}
}
