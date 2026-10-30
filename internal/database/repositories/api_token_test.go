package repositories

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestAPITokenRepository_HintAndRotate(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	users := NewUserRepository(db, nil)
	owner := &models.User{Email: uniqueEmail("apitoken-owner")}
	other := &models.User{Email: uniqueEmail("apitoken-other")}
	for _, u := range []*models.User{owner, other} {
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	repo := NewAPITokenRepository(db)
	exp := sql.NullTime{Time: time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second), Valid: true}
	tok := &models.APIToken{UserID: owner.ID, Name: "mcp", TokenHash: "hash-" + uniqueID(), TokenHint: "gf_\u2026aaaa",
		Scopes: []string{models.ScopeRead, models.ScopeWrite}, ExpiresAt: exp, CreatedAt: time.Now()}
	if err := repo.Create(ctx, tok); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.GetByTokenHash(ctx, tok.TokenHash)
	if err != nil || got.TokenHint != "gf_\u2026aaaa" {
		t.Fatalf("GetByTokenHash = %+v, %v", got, err)
	}
	if err = repo.UpdateLastUsed(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}

	newHash := "hash-" + uniqueID()
	if err = repo.Rotate(ctx, tok.ID, other.ID, newHash, "gf_\u2026bbbb"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotate by another user = %v, want ErrNotFound", err)
	}
	if err = repo.Rotate(ctx, tok.ID, owner.ID, newHash, "gf_\u2026bbbb"); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err = repo.GetByTokenHash(ctx, tok.TokenHash); err == nil {
		t.Fatal("old hash still resolves after rotate")
	}
	after, err := repo.GetByID(ctx, tok.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.TokenHash != newHash || after.TokenHint != "gf_\u2026bbbb" || after.Name != "mcp" ||
		len(after.Scopes) != 2 || !after.ExpiresAt.Time.Equal(exp.Time) || after.LastUsedAt.Valid {
		t.Fatalf("after rotate = %+v", after)
	}
	list, err := repo.ListByUserID(ctx, owner.ID)
	if err != nil || len(list) != 1 || list[0].TokenHint != "gf_\u2026bbbb" {
		t.Fatalf("list = %v, %v", list, err)
	}
}
