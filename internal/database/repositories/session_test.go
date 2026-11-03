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

func TestSessionRepository_DeleteByUserIDExcept(t *testing.T) {
	db := getTestDB(t)
	ctx := context.Background()
	users := NewUserRepository(db, nil)
	sessions := NewSessionRepository(db)

	owner := &models.User{Email: uniqueEmail("session-revoke")}
	other := &models.User{Email: uniqueEmail("session-revoke-other")}
	for _, u := range []*models.User{owner, other} {
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	newSession := func(userID, label string) *models.Session {
		t.Helper()
		s := &models.Session{UserID: userID, TokenHash: label + "-" + uniqueID(), ExpiresAt: time.Now().Add(time.Hour)}
		if err := sessions.Create(ctx, s); err != nil {
			t.Fatalf("create session: %v", err)
		}
		return s
	}
	keep := newSession(owner.ID, "keep")
	gone1 := newSession(owner.ID, "gone1")
	gone2 := newSession(owner.ID, "gone2")
	otherUser := newSession(other.ID, "other")

	n, err := sessions.DeleteByUserIDExcept(ctx, owner.ID, keep.ID)
	if err != nil {
		t.Fatalf("DeleteByUserIDExcept: %v", err)
	}
	if n != 2 {
		t.Errorf("removed %d rows, want 2", n)
	}
	if _, err := sessions.GetByTokenHash(ctx, keep.TokenHash); err != nil {
		t.Errorf("kept session removed: %v", err)
	}
	for _, s := range []*models.Session{gone1, gone2} {
		if _, err := sessions.GetByTokenHash(ctx, s.TokenHash); !errors.Is(err, ErrNotFound) {
			t.Errorf("session %s still present (err=%v)", s.TokenHash, err)
		}
	}
	if _, err := sessions.GetByTokenHash(ctx, otherUser.TokenHash); err != nil {
		t.Errorf("another user's session removed: %v", err)
	}

	// Second call has nothing left to remove.
	if n, err := sessions.DeleteByUserIDExcept(ctx, owner.ID, keep.ID); err != nil || n != 0 {
		t.Errorf("second call: n=%d err=%v, want 0, nil", n, err)
	}

	// An empty keep id removes every session of the user.
	if n, err := sessions.DeleteByUserIDExcept(ctx, owner.ID, ""); err != nil || n != 1 {
		t.Errorf("empty keep: n=%d err=%v, want 1, nil", n, err)
	}
	if _, err := sessions.GetByTokenHash(ctx, keep.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty keep left the session (err=%v)", err)
	}
	if _, err := sessions.GetByTokenHash(ctx, otherUser.TokenHash); err != nil {
		t.Errorf("another user's session removed by empty keep: %v", err)
	}
}
