package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

func TestSessionRepository_DeleteExpired(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	users := NewUserRepository(db, nil)
	sessions := NewSessionRepository(db)

	user := &models.User{Email: uniqueEmail("session-purge")}
	if err := users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	expired := &models.Session{UserID: user.ID, TokenHash: "expired-" + uniqueID(), ExpiresAt: time.Now().Add(-time.Hour)}
	live := &models.Session{UserID: user.ID, TokenHash: "live-" + uniqueID(), ExpiresAt: time.Now().Add(time.Hour)}
	for _, s := range []*models.Session{expired, live} {
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}

	n, err := sessions.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n < 1 {
		t.Errorf("DeleteExpired removed %d rows, want >= 1", n)
	}
	if _, err := sessions.GetByTokenHash(ctx, expired.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired row still present (err=%v)", err)
	}
	if _, err := sessions.GetByTokenHash(ctx, live.TokenHash); err != nil {
		t.Errorf("live row removed: %v", err)
	}
}
