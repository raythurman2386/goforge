package auth

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// memTwoFactorStore is an in-memory TwoFactorStore with the same
// single-use semantics as the Postgres repository.
type memTwoFactorStore struct {
	mu         sync.Mutex
	totp       map[string]*models.UserTOTP
	codes      map[string]map[string]bool // user -> hash -> used
	challenges map[string]*models.LoginChallenge
	nextID     int
}

func newMemTwoFactorStore() *memTwoFactorStore {
	return &memTwoFactorStore{
		totp:       map[string]*models.UserTOTP{},
		codes:      map[string]map[string]bool{},
		challenges: map[string]*models.LoginChallenge{},
	}
}

func (m *memTwoFactorStore) GetTOTP(_ context.Context, userID string) (*models.UserTOTP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.totp[userID]
	if !ok {
		return nil, repositories.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *memTwoFactorStore) SavePendingTOTP(_ context.Context, userID, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.totp[userID]; ok && t.Enabled() {
		return repositories.ErrAlreadyExists
	}
	m.totp[userID] = &models.UserTOTP{UserID: userID, Secret: secret}
	return nil
}

func (m *memTwoFactorStore) EnableTOTP(_ context.Context, userID string, step int64, hashes []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.totp[userID]
	if !ok || t.Enabled() {
		return repositories.ErrNotFound
	}
	t.EnabledAt = sql.NullTime{Time: time.Now(), Valid: true}
	t.LastUsedStep = step
	m.codes[userID] = map[string]bool{}
	for _, h := range hashes {
		m.codes[userID][h] = false
	}
	return nil
}

func (m *memTwoFactorStore) UseTOTPStep(_ context.Context, userID string, step int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.totp[userID]
	if !ok || !t.Enabled() || t.LastUsedStep >= step {
		return false, nil
	}
	t.LastUsedStep = step
	return true, nil
}

func (m *memTwoFactorStore) UseRecoveryCode(_ context.Context, userID, hash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	used, ok := m.codes[userID][hash]
	if !ok || used {
		return false, nil
	}
	m.codes[userID][hash] = true
	return true, nil
}

func (m *memTwoFactorStore) CountUnusedRecoveryCodes(_ context.Context, userID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, used := range m.codes[userID] {
		if !used {
			n++
		}
	}
	return n, nil
}

func (m *memTwoFactorStore) DeleteTOTP(_ context.Context, userID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.totp[userID]
	delete(m.totp, userID)
	delete(m.codes, userID)
	for id, c := range m.challenges {
		if c.UserID == userID {
			delete(m.challenges, id)
		}
	}
	return ok, nil
}

func (m *memTwoFactorStore) CreateChallenge(_ context.Context, c *models.LoginChallenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	c.ID = string(rune('a' + m.nextID))
	cp := *c
	m.challenges[c.ID] = &cp
	return nil
}

func (m *memTwoFactorStore) GetChallengeByTokenHash(_ context.Context, hash string) (*models.LoginChallenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.challenges {
		if c.TokenHash == hash {
			cp := *c
			return &cp, nil
		}
	}
	return nil, repositories.ErrNotFound
}

func (m *memTwoFactorStore) IncrementChallengeAttempts(_ context.Context, id string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.challenges[id]
	if !ok {
		return 0, repositories.ErrNotFound
	}
	c.Attempts++
	return c.Attempts, nil
}

func (m *memTwoFactorStore) DeleteChallenge(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.challenges[id]
	delete(m.challenges, id)
	return ok, nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func enrolled(t *testing.T) (*TwoFactor, *fakeClock, string, []string) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	tf := NewTwoFactor(newMemTwoFactorStore()).WithClock(clk.now)
	ctx := context.Background()
	user := &models.User{ID: "u1", Email: "a@example.com"}
	en, err := tf.BeginEnrollment(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := TOTPCode(en.Secret, TOTPStep(clk.t))
	codes, err := tf.ConfirmEnrollment(ctx, user.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	return tf, clk, en.Secret, codes
}

func TestTwoFactorEnrollmentFlow(t *testing.T) {
	ctx := context.Background()
	clk := &fakeClock{t: time.Unix(1_800_000_000, 0)}
	tf := NewTwoFactor(newMemTwoFactorStore()).WithClock(clk.now)
	user := &models.User{ID: "u1", Email: "a@example.com"}

	if on, _ := tf.Enabled(ctx, user.ID); on {
		t.Fatal("enabled before enrollment")
	}
	if _, err := tf.ConfirmEnrollment(ctx, user.ID, "123456"); !errors.Is(err, ErrNoPendingEnrollment) {
		t.Fatalf("confirm without begin: %v", err)
	}
	en, err := tf.BeginEnrollment(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if on, _ := tf.Enabled(ctx, user.ID); on {
		t.Fatal("pending enrollment counted as enabled")
	}
	if _, cerr := tf.ConfirmEnrollment(ctx, user.ID, "000000"); !errors.Is(cerr, ErrInvalidTwoFactorCode) {
		t.Fatalf("wrong first code: %v", cerr)
	}
	code, _ := TOTPCode(en.Secret, TOTPStep(clk.t))
	codes, err := tf.ConfirmEnrollment(ctx, user.ID, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d recovery codes", len(codes))
	}
	st, _ := tf.Status(ctx, user.ID)
	if !st.Enabled || st.RecoveryCodesLeft != RecoveryCodeCount {
		t.Fatalf("status = %+v", st)
	}
	if _, err := tf.BeginEnrollment(ctx, user); !errors.Is(err, ErrTwoFactorAlreadyEnabled) {
		t.Fatalf("begin while enabled: %v", err)
	}
	// The enrollment code's step is spent.
	if _, err := tf.Verify(ctx, user.ID, code); !errors.Is(err, ErrInvalidTwoFactorCode) {
		t.Fatalf("enrollment code replayed: %v", err)
	}
}

func TestTwoFactorVerifyTOTPOncePerStep(t *testing.T) {
	ctx := context.Background()
	tf, clk, secret, _ := enrolled(t)
	clk.advance(TOTPPeriod)
	code, _ := TOTPCode(secret, TOTPStep(clk.t))
	m, err := tf.Verify(ctx, "u1", code)
	if err != nil || m != models.SecondFactorTOTP {
		t.Fatalf("Verify = (%q, %v)", m, err)
	}
	if _, err := tf.Verify(ctx, "u1", code); !errors.Is(err, ErrInvalidTwoFactorCode) {
		t.Fatalf("replayed code: %v", err)
	}
	// A code from 30 s ago (still within skew) is older than the used step.
	prev, _ := TOTPCode(secret, TOTPStep(clk.t)-1)
	if _, err := tf.Verify(ctx, "u1", prev); !errors.Is(err, ErrInvalidTwoFactorCode) {
		t.Fatalf("older step accepted: %v", err)
	}
	// Skew: the next step's code is accepted early.
	next, _ := TOTPCode(secret, TOTPStep(clk.t)+1)
	if _, err := tf.Verify(ctx, "u1", next); err != nil {
		t.Fatalf("next-step code: %v", err)
	}
}

func TestTwoFactorRecoveryCodeSingleUse(t *testing.T) {
	ctx := context.Background()
	tf, _, _, codes := enrolled(t)
	m, err := tf.Verify(ctx, "u1", codes[0])
	if err != nil || m != models.SecondFactorRecoveryCode {
		t.Fatalf("Verify = (%q, %v)", m, err)
	}
	if _, err := tf.Verify(ctx, "u1", codes[0]); !errors.Is(err, ErrInvalidTwoFactorCode) {
		t.Fatalf("recovery code reused: %v", err)
	}
	// Typed in upper case without the dash still matches a fresh code.
	in := codes[1][:5] + codes[1][6:]
	if _, err := tf.Verify(ctx, "u1", toUpper(in)); err != nil {
		t.Fatalf("normalized recovery code: %v", err)
	}
	if _, err := tf.Verify(ctx, "u1", "zzzzz-zzzzz"); !errors.Is(err, ErrInvalidTwoFactorCode) {
		t.Fatalf("unknown recovery code: %v", err)
	}
	st, _ := tf.Status(ctx, "u1")
	if st.RecoveryCodesLeft != RecoveryCodeCount-2 {
		t.Fatalf("codes left = %d", st.RecoveryCodesLeft)
	}
}

func TestTwoFactorDisableAndReset(t *testing.T) {
	ctx := context.Background()
	tf, _, _, codes := enrolled(t)
	if _, err := tf.Disable(ctx, "u1", "000000"); !errors.Is(err, ErrInvalidTwoFactorCode) {
		t.Fatalf("disable with wrong code: %v", err)
	}
	if on, _ := tf.Enabled(ctx, "u1"); !on {
		t.Fatal("wrong code disabled 2FA")
	}
	if _, err := tf.Disable(ctx, "u1", codes[2]); err != nil {
		t.Fatal(err)
	}
	if on, _ := tf.Enabled(ctx, "u1"); on {
		t.Fatal("still enabled after disable")
	}
	if _, err := tf.Verify(ctx, "u1", codes[3]); !errors.Is(err, ErrTwoFactorNotEnabled) {
		t.Fatalf("recovery code after disable: %v", err)
	}

	tf2, _, _, _ := enrolled(t)
	if ok, err := tf2.Reset(ctx, "u1"); err != nil || !ok {
		t.Fatalf("Reset = (%v, %v)", ok, err)
	}
	if on, _ := tf2.Enabled(ctx, "u1"); on {
		t.Fatal("still enabled after reset")
	}
}

func TestLoginChallengeExpiry(t *testing.T) {
	ctx := context.Background()
	tf, clk, _, _ := enrolled(t)
	token, c, err := tf.StartChallenge(ctx, "u1", true)
	if err != nil {
		t.Fatal(err)
	}
	if c.TokenHash == token || c.TokenHash != hashToken(token) {
		t.Fatal("challenge must store only the token hash")
	}
	got, err := tf.Challenge(ctx, token)
	if err != nil || !got.RememberMe || got.UserID != "u1" {
		t.Fatalf("Challenge = (%+v, %v)", got, err)
	}
	clk.advance(LoginChallengeTTL - time.Second)
	if _, err := tf.Challenge(ctx, token); err != nil {
		t.Fatalf("challenge expired early: %v", err)
	}
	clk.advance(time.Second)
	if _, err := tf.Challenge(ctx, token); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("challenge at TTL: %v", err)
	}
	// The expired challenge was deleted, not just hidden.
	clk.advance(-time.Hour)
	if _, err := tf.Challenge(ctx, token); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("expired challenge came back: %v", err)
	}
	if _, err := tf.Challenge(ctx, ""); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("empty token: %v", err)
	}
}

func TestLoginChallengeAttemptsAndSingleUse(t *testing.T) {
	ctx := context.Background()
	tf, _, _, _ := enrolled(t)
	token, c, _ := tf.StartChallenge(ctx, "u1", false)
	for i := 1; i < MaxLoginChallengeAttempts; i++ {
		n, ended, err := tf.ChallengeFailed(ctx, c)
		if err != nil || ended || n != i {
			t.Fatalf("attempt %d: (%d, %v, %v)", i, n, ended, err)
		}
	}
	if _, ended, _ := tf.ChallengeFailed(ctx, c); !ended {
		t.Fatal("challenge did not end at the attempt limit")
	}
	if _, err := tf.Challenge(ctx, token); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("challenge after limit: %v", err)
	}

	token, _, _ = tf.StartChallenge(ctx, "u1", false)
	c, _ = tf.Challenge(ctx, token)
	if err := tf.CompleteChallenge(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := tf.CompleteChallenge(ctx, c); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("challenge spent twice: %v", err)
	}
	if _, err := tf.Challenge(ctx, token); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("spent challenge still live: %v", err)
	}
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
