package repositories

import (
	"context"
	"errors"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

// backup_enabled round-trips through Create, GetByID, ListByUserID, and
// ListAll. SetBackupEnabled flips it. Update of status leaves it alone.
func TestServiceInstanceRepository_BackupEnabled(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("svc-backup")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewServiceInstanceRepository(db)
	on := &models.ServiceInstance{
		UserID: user.ID, TemplateName: "gitea", Name: "git-" + uniqueID(),
		Status: models.ServiceStatusRunning, BackupEnabled: true,
	}
	off := &models.ServiceInstance{
		UserID: user.ID, TemplateName: "redis", Name: "cache-" + uniqueID(),
		Status: models.ServiceStatusRunning,
	}
	if err := repo.Create(ctx, on); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, off); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByID(ctx, on.ID)
	if err != nil || !got.BackupEnabled {
		t.Fatalf("GetByID backup_enabled = %v (%v), want true", got != nil && got.BackupEnabled, err)
	}
	gotOff, err := repo.GetByID(ctx, off.ID)
	if err != nil || gotOff.BackupEnabled {
		t.Fatalf("GetByID redis backup_enabled = %v (%v), want false", gotOff != nil && gotOff.BackupEnabled, err)
	}

	list, err := repo.ListByUserID(ctx, user.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListByUserID = %v (%v)", list, err)
	}
	byID := map[string]bool{}
	for _, s := range list {
		byID[s.ID] = s.BackupEnabled
	}
	if !byID[on.ID] || byID[off.ID] {
		t.Fatalf("ListByUserID flags = %v", byID)
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawOn, sawOff bool
	for _, s := range all {
		switch s.ID {
		case on.ID:
			sawOn = s.BackupEnabled
		case off.ID:
			sawOff = s.BackupEnabled
		}
	}
	if !sawOn || sawOff {
		t.Fatalf("ListAll on=%v off=%v", sawOn, sawOff)
	}

	on.Status = models.ServiceStatusStopped
	if err := repo.Update(ctx, on); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.GetByID(ctx, on.ID); !again.BackupEnabled {
		t.Fatal("Update cleared backup_enabled")
	}

	if err := repo.SetBackupEnabled(ctx, on.ID, false); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.GetByID(ctx, on.ID); again.BackupEnabled {
		t.Fatal("SetBackupEnabled(false) did not clear the flag")
	}
	if err := repo.SetBackupEnabled(ctx, on.ID, true); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.GetByID(ctx, on.ID); !again.BackupEnabled {
		t.Fatal("SetBackupEnabled(true) did not set the flag")
	}

	if err := repo.SetBackupEnabled(ctx, "00000000-0000-0000-0000-000000000000", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetBackupEnabled on a missing id = %v, want ErrNotFound", err)
	}
}
