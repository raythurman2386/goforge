package repositories

import (
	"context"
	"sync"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestServiceInstanceRepository_UpdateIfStatus(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("svc-instance-cas")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	s := &models.ServiceInstance{
		UserID:       user.ID,
		TemplateName: "postgres",
		Name:         "cas-" + uniqueID(),
		Status:       models.ServiceStatusRunning,
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	claim := *s
	claim.Status = models.ServiceStatusPending
	claim.Name = "renamed"
	ok, err := repo.UpdateIfStatus(ctx, &claim, models.ServiceStatusRunning)
	if err != nil || !ok {
		t.Fatalf("UpdateIfStatus(running) = %v, %v; want true, nil", ok, err)
	}

	// The stored status is pending now, so a second claim expecting running loses.
	stale := *s
	stale.Status = models.ServiceStatusPending
	stale.Name = "stale"
	ok, err = repo.UpdateIfStatus(ctx, &stale, models.ServiceStatusRunning)
	if err != nil || ok {
		t.Fatalf("stale UpdateIfStatus = %v, %v; want false, nil", ok, err)
	}
	got, err := repo.GetByID(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.ServiceStatusPending || got.Name != "renamed" {
		t.Errorf("stored = %s/%q, want pending/renamed (stale write discarded)", got.Status, got.Name)
	}

	missing := *s
	missing.ID = "00000000-0000-0000-0000-000000000000"
	if ok, err = repo.UpdateIfStatus(ctx, &missing, models.ServiceStatusPending); err != nil || ok {
		t.Errorf("missing row UpdateIfStatus = %v, %v; want false, nil", ok, err)
	}
}

// Many callers that all read the same status race on one row; Postgres lets
// exactly one of them swap it.
func TestServiceInstanceRepository_UpdateIfStatus_ConcurrentOneWins(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)

	repo := NewServiceInstanceRepository(db)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("svc-instance-cas-race")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	s := &models.ServiceInstance{
		UserID:       user.ID,
		TemplateName: "postgres",
		Name:         "cas-race-" + uniqueID(),
		Status:       models.ServiceStatusFailed,
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	const n = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		wins  int
		start = make(chan struct{})
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claim := *s
			claim.Status = models.ServiceStatusPending
			<-start
			ok, err := repo.UpdateIfStatus(ctx, &claim, models.ServiceStatusFailed)
			if err != nil {
				t.Errorf("UpdateIfStatus: %v", err)
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1", wins)
	}
}
