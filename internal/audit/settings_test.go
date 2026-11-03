package audit

import (
	"context"
	"errors"
	"testing"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

func TestParseRetentionDays(t *testing.T) {
	t.Parallel()
	ok := []string{"0", "30", "365", "3650"}
	for _, raw := range ok {
		n, err := ParseRetentionDays(raw)
		if err != nil {
			t.Errorf("ParseRetentionDays(%q) error = %v", raw, err)
		}
		if _, perr := ParseRetentionDays("  " + raw + " "); perr != nil {
			t.Errorf("trimmed ParseRetentionDays(%q) error = %v", raw, perr)
		}
		_ = n
	}
	for _, raw := range []string{"", "10", "29", "3651", "-1", "abc", "30.5"} {
		_, err := ParseRetentionDays(raw)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "retention_days" {
			t.Errorf("ParseRetentionDays(%q) err = %v, want field error", raw, err)
		}
	}
}

func TestRetentionNote(t *testing.T) {
	t.Parallel()
	if got := RetentionNote(0); got != "Rows are kept until deleted by hand." {
		t.Errorf("forever note = %q", got)
	}
	if got := RetentionNote(30); got != "Rows older than 30 days are deleted daily." {
		t.Errorf("30 note = %q", got)
	}
}

func TestIntegrityLine(t *testing.T) {
	t.Parallel()
	if got := (IntegrityView{}).Line(); got != "Integrity: check pending" {
		t.Errorf("pending = %q", got)
	}
	if got := (IntegrityView{Checked: true, OK: false, Seq: 4}).Line(); got != "Integrity check failed at #4" {
		t.Errorf("failed = %q", got)
	}
	ok := IntegrityView{Checked: true, OK: true, Seq: 9}
	if got := ok.Line(); len(got) < len("Integrity: verified through #9") || got[:len("Integrity: verified through #9")] != "Integrity: verified through #9" {
		t.Errorf("ok = %q", got)
	}
}

type memStore struct {
	row *models.AuditSettings
	err error
}

func (m *memStore) Get(context.Context) (*models.AuditSettings, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.row == nil {
		return nil, repositories.ErrNotFound
	}
	cp := *m.row
	return &cp, nil
}

func (m *memStore) SaveRetention(_ context.Context, days int, updatedBy string) error {
	if m.row == nil {
		m.row = &models.AuditSettings{}
	}
	m.row.RetentionDays = days
	m.row.RetentionSaved = true
	m.row.UpdatedBy.String = updatedBy
	m.row.UpdatedBy.Valid = updatedBy != ""
	return nil
}

func TestSettings_EnvFallbackAndSave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &memStore{}
	svc := &SettingsService{Store: store, EnvDays: 90, EnvSet: true}

	days, source, effErr := svc.Effective(ctx)
	if effErr != nil || days != 90 || source != SourceEnvironment {
		t.Fatalf("effective = %d %s %v", days, source, effErr)
	}
	view, viewErr := svc.View(ctx)
	if viewErr != nil || !view.ReadOnly {
		t.Fatalf("view = %+v %v", view, viewErr)
	}
	if _, _, _, err := svc.Save(ctx, "30", "user"); err == nil {
		t.Fatal("env source was writable")
	}

	store.row = nil
	svc.EnvSet = false
	if _, _, changed, saveErr := svc.Save(ctx, "30", "admin"); saveErr != nil || !changed {
		t.Fatalf("save 30: changed=%v err=%v", changed, saveErr)
	}
	days, source, effErr = svc.Effective(ctx)
	if effErr != nil || days != 30 || source != SourceSettings {
		t.Fatalf("after save = %d %s %v", days, source, effErr)
	}
	if _, _, changed, saveErr := svc.Save(ctx, "30", "admin"); saveErr != nil || changed {
		t.Fatalf("second save changed=%v err=%v", changed, saveErr)
	}

	svc.EnvSet = true
	svc.EnvDays = 10
	if checkErr := CheckEnv(ctx, store, errors.New("bad env")); checkErr != nil {
		t.Fatalf("saved row should ignore bad env: %v", checkErr)
	}
	days, source, effErr = svc.Effective(ctx)
	if effErr != nil || days != 30 || source != SourceSettings {
		t.Fatalf("saved wins = %d %s %v", days, source, effErr)
	}
}

func TestCheckEnv_NoRow(t *testing.T) {
	t.Parallel()
	envErr := errors.New("bad env")
	if err := CheckEnv(context.Background(), &memStore{}, envErr); !errors.Is(err, envErr) {
		t.Fatalf("CheckEnv = %v", err)
	}
	if err := CheckEnv(context.Background(), &memStore{}, nil); err != nil {
		t.Fatalf("unset env: %v", err)
	}
}

func TestDeriveChainKey(t *testing.T) {
	t.Parallel()
	a, keyErr := DeriveChainKey("one-key")
	if keyErr != nil || len(a) != ChainKeyLen {
		t.Fatalf("key = %d %v", len(a), keyErr)
	}
	b, sameErr := DeriveChainKey("one-key")
	if sameErr != nil || string(a) != string(b) {
		t.Fatal("same encryption key must derive the same chain key")
	}
	c, otherErr := DeriveChainKey("other-key")
	if otherErr != nil || string(a) == string(c) {
		t.Fatal("different encryption keys must derive different chain keys")
	}
	if _, emptyErr := DeriveChainKey(""); emptyErr == nil {
		t.Fatal("empty key")
	}
	sum1, hashErr := rowHash(a, Genesis(), []byte("canonical"))
	if hashErr != nil || len(sum1) != ChainKeyLen {
		t.Fatalf("rowHash = %d %v", len(sum1), hashErr)
	}
	sum2, hashErr2 := rowHash(a, Genesis(), []byte("canonical-2"))
	if hashErr2 != nil || string(sum1) == string(sum2) {
		t.Fatal("canonical text must change the digest")
	}
}

func TestFormatVerifyLines(t *testing.T) {
	t.Parallel()
	if got := FormatVerified(3, []byte{0xab}); got != "audit chain verified head_seq=3 head_hash=ab" {
		t.Errorf("verified = %q", got)
	}
	fail := &ChainError{Seq: 2, Reason: ReasonHashMismatch}
	if got := fail.Error(); got != "audit chain failed seq=2 reason=hash mismatch" {
		t.Errorf("failed = %q", got)
	}
}
