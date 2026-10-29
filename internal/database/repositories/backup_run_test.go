package repositories

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestBackupRunRepository_Lifecycle(t *testing.T) {
	db := getTestDB(t)
	repo := NewBackupRunRepository(db)
	ctx := context.Background()

	unlock, err := repo.Lock(ctx)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if _, err2 := repo.Lock(ctx); !errors.Is(err2, ErrBackupLocked) {
		t.Fatalf("second Lock = %v, want ErrBackupLocked", err2)
	}
	defer unlock()

	if _, err = repo.MarkInterrupted(ctx); err != nil {
		t.Fatal(err)
	}

	run := &models.BackupRun{Trigger: models.BackupTriggerManual, RunKey: "2026-10-29/20261029T190000Z-" + testRunID, StartedAt: time.Now().Add(time.Hour)}
	if err = repo.Start(ctx, run); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM backup_runs WHERE id = $1`, run.ID) })

	run.Status = models.BackupRunSucceeded
	run.Objects, run.Bytes, run.Skipped, run.Pruned = 4, 1234, 1, 2
	if err = repo.Finish(ctx, run); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err := repo.LatestSucceeded(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != run.ID || got.Objects != 4 || got.Bytes != 1234 || got.Pruned != 2 || !got.FinishedAt.Valid {
		t.Errorf("LatestSucceeded = %+v", got)
	}

	stale := &models.BackupRun{Trigger: models.BackupTriggerSchedule, RunKey: "stale-" + testRunID, StartedAt: time.Now().Add(2 * time.Hour)}
	if err = repo.Start(ctx, stale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM backup_runs WHERE id = $1`, stale.ID) })
	if n, mErr := repo.MarkInterrupted(ctx); mErr != nil || n != 1 {
		t.Fatalf("MarkInterrupted = %d, %v", n, mErr)
	}
	list, err := repo.List(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != stale.ID || list[0].Status != models.BackupRunFailed || !list[0].ErrorSummary.Valid {
		t.Errorf("List = %+v", list)
	}
	if err = repo.Finish(ctx, &models.BackupRun{ID: "00000000-0000-0000-0000-000000000000", Status: models.BackupRunFailed, ErrorSummary: sql.NullString{}}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Finish unknown = %v", err)
	}
}
