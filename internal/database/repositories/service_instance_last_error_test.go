package repositories

import (
	"context"
	"database/sql"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

// last_error round-trips through Update, UpdateIfStatus, GetByID, and
// ListByUserID, and an empty value is stored as NULL.
func TestServiceInstanceRepository_LastError(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("svc-lasterr")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewServiceInstanceRepository(db)
	inst := &models.ServiceInstance{UserID: user.ID, TemplateName: "postgres", Name: "db-" + uniqueID(), Status: models.ServiceStatusPending}
	if err := repo.Create(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByID(ctx, inst.ID); got.LastError.Valid {
		t.Fatalf("new instance last_error = %+v, want NULL", got.LastError)
	}

	inst.Status = models.ServiceStatusFailed
	inst.LastError = sql.NullString{String: "image pull failed for postgres:99: manifest unknown", Valid: true}
	if err := repo.Update(ctx, inst); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, inst.ID)
	if err != nil || got.LastError != inst.LastError {
		t.Fatalf("GetByID last_error = %+v (%v), want %+v", got.LastError, err, inst.LastError)
	}
	list, err := repo.ListByUserID(ctx, user.ID)
	if err != nil || len(list) != 1 || list[0].LastError != inst.LastError {
		t.Fatalf("ListByUserID = %v (%v)", list, err)
	}

	got.Status = models.ServiceStatusPending
	got.LastError = sql.NullString{}
	if ok, err := repo.UpdateIfStatus(ctx, got, models.ServiceStatusFailed); !ok || err != nil {
		t.Fatalf("UpdateIfStatus = %v, %v", ok, err)
	}
	if again, _ := repo.GetByID(ctx, inst.ID); again.LastError.Valid {
		t.Errorf("last_error after clear = %+v, want NULL", again.LastError)
	}
}
