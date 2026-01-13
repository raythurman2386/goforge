package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raythurman2386/gocool/internal/database/repositories"
	"github.com/raythurman2386/gocool/internal/models"
)

func TestAuthMiddleware(t *testing.T) {
	db := setupSessionTestDB(t)
	defer db.Close()

	userRepo := repositories.NewUserRepository(db)
	sessionRepo := repositories.NewSessionRepository(db)
	manager := NewSessionManager(sessionRepo, userRepo)

	middleware := Middleware(manager)

	// Create a user and session
	user := &models.User{Email: "middleware@example.com"}
	_ = userRepo.Create(context.Background(), user)
	_, token, _ := manager.CreateSession(context.Background(), user.ID, false)

	// Test handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser := r.Context().Value(UserContextKey)
		if gotUser == nil {
			t.Error("Middleware did not add user to context")
		}
	})

	// Create request with session cookie
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	middleware(nextHandler).ServeHTTP(rr, req)
}

func TestRequireAuth(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: Unauthenticated
	req1 := httptest.NewRequest("GET", "/", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusSeeOther {
		t.Errorf("RequireAuth unauthenticated code = %d, want %d", rr1.Code, http.StatusSeeOther)
	}

	// Case 2: Authenticated
	req2 := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req2.Context(), UserContextKey, &models.User{})
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2.WithContext(ctx))
	if rr2.Code != http.StatusOK {
		t.Errorf("RequireAuth authenticated code = %d, want %d", rr2.Code, http.StatusOK)
	}
}
