package repositories

import (
	"context"
	"errors"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
)

// One approval_ref with one idempotency_key is stored once per user and
// action (migration 023); FindApproval and FindOutcome find the rows.
func TestAuditLogRepository_ApprovalIdempotency(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()

	user := &models.User{Email: uniqueEmail("audit-idem")}
	if err := NewUserRepository(db, nil).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewAuditLogRepository(db)
	ref := "ref-" + uniqueID()
	row := func(action, details string) *models.AuditLog {
		l := &models.AuditLog{Action: action, ResourceType: models.ResourceTypeProject, Details: []byte(details)}
		l.UserID.String, l.UserID.Valid = user.ID, true
		return l
	}

	begin := row("api_v1.trigger_deployment", `{"approval_ref":"`+ref+`","idempotency_key":"k1"}`)
	if err := repo.Create(ctx, begin); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, row("api_v1.trigger_deployment", `{"approval_ref":"`+ref+`","idempotency_key":"k1"}`)); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate insert error = %v, want ErrAlreadyExists", err)
	}
	if err := repo.Create(ctx, row("api_v1.trigger_deployment", `{"approval_ref":"`+ref+`","idempotency_key":"k2"}`)); err != nil {
		t.Fatalf("other key: %v", err)
	}

	got, err := repo.FindApproval(ctx, user.ID, "api_v1.trigger_deployment", ref, "k1")
	if err != nil || got == nil || got.ID != begin.ID {
		t.Fatalf("FindApproval = %+v, %v", got, err)
	}
	if miss, missErr := repo.FindApproval(ctx, user.ID, "api_v1.trigger_deployment", ref, "k9"); missErr != nil || miss != nil {
		t.Fatalf("FindApproval miss = %+v, %v", miss, missErr)
	}

	if early, earlyErr := repo.FindOutcome(ctx, user.ID, "api_v1.trigger_deployment", begin.ID); earlyErr != nil || early != nil {
		t.Fatalf("FindOutcome before finish = %+v, %v", early, earlyErr)
	}
	done := row("api_v1.trigger_deployment.completed", `{"approval_ref":"`+ref+`","audit_id":"`+begin.ID+`"}`)
	if err = repo.Create(ctx, done); err != nil {
		t.Fatal(err)
	}
	got, err = repo.FindOutcome(ctx, user.ID, "api_v1.trigger_deployment", begin.ID)
	if err != nil || got == nil || got.ID != done.ID {
		t.Fatalf("FindOutcome = %+v, %v", got, err)
	}
}
