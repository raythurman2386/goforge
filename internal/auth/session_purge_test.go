package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestSessionManager_PurgeExpired(t *testing.T) {
	db := setupSessionTestDB(t)
	defer db.Close()

	userRepo := repositories.NewUserRepository(db, nil)
	sessionRepo := repositories.NewSessionRepository(db)
	manager := NewSessionManager(sessionRepo, userRepo, nil)
	ctx := context.Background()

	user := &models.User{Email: "purge@example.com"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	expired := &models.Session{UserID: user.ID, TokenHash: hashToken("old"), ExpiresAt: time.Now().Add(-time.Hour)}
	live := &models.Session{UserID: user.ID, TokenHash: hashToken("new"), ExpiresAt: time.Now().Add(time.Hour)}
	for _, s := range []*models.Session{expired, live} {
		if err := sessionRepo.Create(ctx, s); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}

	n, err := manager.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("PurgeExpired removed %d rows, want 1", n)
	}
	if _, err := sessionRepo.GetByTokenHash(ctx, expired.TokenHash); !errors.Is(err, repositories.ErrNotFound) {
		t.Errorf("expired row still present (err=%v)", err)
	}
	if _, err := sessionRepo.GetByTokenHash(ctx, live.TokenHash); err != nil {
		t.Errorf("live row removed: %v", err)
	}
}

// The background loop purges once at start, without waiting for the first tick,
// and returns when ctx is canceled.
func TestSessionManager_RunExpiredSessionPurge(t *testing.T) {
	db := setupSessionTestDB(t)
	defer db.Close()
	// Each sqlite :memory: connection is its own database; keep one so the
	// purge goroutine sees the tables created above.
	db.SetMaxOpenConns(1)

	userRepo := repositories.NewUserRepository(db, nil)
	sessionRepo := repositories.NewSessionRepository(db)
	manager := NewSessionManager(sessionRepo, userRepo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	user := &models.User{Email: "loop@example.com"}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	expired := &models.Session{UserID: user.ID, TokenHash: hashToken("stale"), ExpiresAt: time.Now().Add(-time.Minute)}
	if err := sessionRepo.Create(ctx, expired); err != nil {
		t.Fatalf("create session: %v", err)
	}

	done := make(chan struct{})
	go func() {
		manager.RunExpiredSessionPurge(ctx, time.Hour)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := sessionRepo.GetByTokenHash(context.Background(), expired.TokenHash)
		if errors.Is(err, repositories.ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expired session not purged at startup (err=%v)", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunExpiredSessionPurge did not return after cancel")
	}
}
