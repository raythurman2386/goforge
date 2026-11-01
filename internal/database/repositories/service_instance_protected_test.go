package repositories

import (
	"context"
	"errors"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

// protected defaults to false, SetProtected flips it, GetByID and
// ListByUserID read it, and Update (config or status) leaves it alone.
func TestServiceInstanceRepository_Protected(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("svc-protected")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewServiceInstanceRepository(db)
	inst := &models.ServiceInstance{UserID: user.ID, TemplateName: "gitea", Name: "git-" + uniqueID(), Status: models.ServiceStatusRunning}
	if err := repo.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByID(ctx, inst.ID); got.Protected {
		t.Fatal("new instance is protected, want false")
	}

	if err := repo.SetProtected(ctx, inst.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, inst.ID)
	if err != nil || !got.Protected {
		t.Fatalf("GetByID protected = %v (%v), want true", got != nil && got.Protected, err)
	}
	list, err := repo.ListByUserID(ctx, user.ID)
	if err != nil || len(list) != 1 || !list[0].Protected {
		t.Fatalf("ListByUserID = %v (%v)", list, err)
	}

	// A stale copy written back by Update must not clear the flag.
	inst.Status = models.ServiceStatusStopped
	if err := repo.Update(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.GetByID(ctx, inst.ID); !again.Protected {
		t.Fatal("Update cleared protected")
	}

	if err := repo.SetProtected(ctx, inst.ID, false); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.GetByID(ctx, inst.ID); again.Protected {
		t.Fatal("SetProtected(false) did not clear the flag")
	}

	if err := repo.SetProtected(ctx, "00000000-0000-0000-0000-000000000000", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetProtected on a missing id = %v, want ErrNotFound", err)
	}
}
