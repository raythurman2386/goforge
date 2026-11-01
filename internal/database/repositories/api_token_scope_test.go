package repositories

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// project_ids and service_ids round-trip through Create, GetByTokenHash,
// GetByID, and ListByUserID; a token created without lists, and a row
// inserted without the columns (an existing token at migration 027), read
// back host-wide with empty, non-nil lists.
func TestAPITokenRepository_ResourceScope(t *testing.T) {
	t.Parallel()
	db := getTestDB(t)
	ctx := context.Background()
	owner := &models.User{Email: uniqueEmail("apitoken-scope")}
	if err := NewUserRepository(db, nil).Create(ctx, owner); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repo := NewAPITokenRepository(db)
	const p1, s1, s2 = "33333333-3333-4333-8333-333333333333", "66666666-6666-4666-8666-666666666666", "77777777-7777-4777-8777-777777777777"

	restricted := &models.APIToken{UserID: owner.ID, Name: "scoped", TokenHash: "hash-" + uniqueID(),
		Scopes: []string{models.ScopeRead}, ProjectIDs: []string{p1}, ServiceIDs: []string{s1, s2}, CreatedAt: time.Now()}
	if err := repo.Create(ctx, restricted); err != nil {
		t.Fatalf("create restricted: %v", err)
	}
	hostWide := &models.APIToken{UserID: owner.ID, Name: "host", TokenHash: "hash-" + uniqueID(),
		Scopes: []string{models.ScopeRead}, CreatedAt: time.Now()}
	if err := repo.Create(ctx, hostWide); err != nil {
		t.Fatalf("create host-wide: %v", err)
	}
	legacyHash := "hash-" + uniqueID()
	if _, err := db.ExecContext(ctx, `INSERT INTO api_tokens (user_id, name, token_hash) VALUES ($1, 'legacy', $2)`, owner.ID, legacyHash); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	got, err := repo.GetByTokenHash(ctx, restricted.TokenHash)
	if err != nil || !slices.Equal(got.ProjectIDs, []string{p1}) || !slices.Equal(got.ServiceIDs, []string{s1, s2}) || !got.IsRestricted() {
		t.Fatalf("GetByTokenHash restricted = %+v, %v", got, err)
	}
	if got, err = repo.GetByID(ctx, restricted.ID); err != nil || !slices.Equal(got.ServiceIDs, []string{s1, s2}) {
		t.Fatalf("GetByID restricted = %+v, %v", got, err)
	}
	for _, hash := range []string{hostWide.TokenHash, legacyHash} {
		hw, hwErr := repo.GetByTokenHash(ctx, hash)
		if hwErr != nil || hw.IsRestricted() || hw.ProjectIDs == nil || hw.ServiceIDs == nil {
			t.Fatalf("host-wide %s = %+v, %v; want empty non-nil lists", hash, hw, hwErr)
		}
	}
	list, err := repo.ListByUserID(ctx, owner.ID)
	if err != nil || len(list) != 3 {
		t.Fatalf("ListByUserID = %d, %v", len(list), err)
	}
	restrictedSeen := 0
	for _, tk := range list {
		if tk.IsRestricted() {
			restrictedSeen++
		}
	}
	if restrictedSeen != 1 {
		t.Fatalf("ListByUserID restricted rows = %d, want 1", restrictedSeen)
	}

	// Rotating a restricted token keeps its lists.
	if err = repo.Rotate(ctx, restricted.ID, owner.ID, "hash-"+uniqueID(), "gf_\u2026cccc"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if got, err = repo.GetByID(ctx, restricted.ID); err != nil || !slices.Equal(got.ProjectIDs, []string{p1}) || len(got.ServiceIDs) != 2 {
		t.Fatalf("after rotate = %+v, %v", got, err)
	}

	// The columns refuse a non-UUID.
	if _, err := db.ExecContext(ctx, `UPDATE api_tokens SET project_ids = '{not-a-uuid}' WHERE id = $1`, hostWide.ID); err == nil {
		t.Fatal("project_ids accepted a non-UUID")
	}
}
