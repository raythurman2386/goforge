package errreport

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

type memStore struct {
	mu         sync.Mutex
	row        *models.ErrorReportingSettings
	unreadable bool
}

func (m *memStore) Get(context.Context) (*models.ErrorReportingSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.row == nil {
		return nil, repositories.ErrNotFound
	}
	c := *m.row
	if m.unreadable {
		c.DSN = ""
		return &c, repositories.ErrErrorReportingSettingsUnreadable
	}
	return &c, nil
}

func (m *memStore) Upsert(_ context.Context, s *models.ErrorReportingSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *s
	c.UpdatedAt = time.Now()
	m.row = &c
	m.unreadable = false
	return nil
}

type fakeLive struct {
	applied []Config
	tested  []Config
	testErr error
}

func (f *fakeLive) Apply(c Config) error { f.applied = append(f.applied, c); return nil }
func (f *fakeLive) SendTest(_ context.Context, c Config, _ time.Duration) (TestResult, error) {
	f.tested = append(f.tested, c)
	return TestResult{EventID: "abc123", Status: 200}, f.testErr
}

func TestErrorReportSettings_EnvFallbackOnlyWithoutRow(t *testing.T) {
	st, live := &memStore{}, &fakeLive{}
	s := &SettingsService{Store: st, Live: live, EnvDSN: testDSN, DefaultEnvironment: "production"}
	c, src, err := s.Effective(t.Context())
	if err != nil || c.DSN != testDSN || src != SourceEnv {
		t.Fatalf("no row: %+v %q %v", c, src, err)
	}
	v, _ := s.View(t.Context())
	if !v.DSNSet || v.Source != SourceEnv || v.DSNHint != "https://o1.ingest.sentry.example.test/42" || v.Saved {
		t.Fatalf("env view %+v", v)
	}
	// Saving the env DSN away (blank, nothing stored) turns reporting off
	// even though SENTRY_DSN is still set.
	if _, err := s.Save(t.Context(), SettingsInput{ClearDSN: true}, "u1"); err != nil {
		t.Fatal(err)
	}
	c, src, _ = s.Effective(t.Context())
	if c.DSN != "" || src != SourceNone {
		t.Fatalf("saved blank row: %+v %q, want off", c, src)
	}
	if len(live.applied) != 1 || live.applied[0].DSN != "" {
		t.Fatalf("applied %+v", live.applied)
	}
}

func TestErrorReportSettings_SaveHotAppliesBlankKeepsRemoveClears(t *testing.T) {
	st, live := &memStore{}, &fakeLive{}
	s := &SettingsService{Store: st, Live: live, DefaultEnvironment: "production"}
	changed, err := s.Save(t.Context(), SettingsInput{DSN: " " + testDSN + " ", Environment: "staging"}, "u1")
	if err != nil || strings.Join(changed, ",") != "dsn,environment" {
		t.Fatalf("save: %v %v", changed, err)
	}
	if st.row.DSN != testDSN || st.row.UpdatedBy.String != "u1" {
		t.Fatalf("stored %+v", st.row)
	}
	if len(live.applied) != 1 || live.applied[0] != (Config{DSN: testDSN, Environment: "staging"}) {
		t.Fatalf("applied %+v", live.applied)
	}
	v, _ := s.View(t.Context())
	if !v.DSNSet || v.Source != SourceSettings || v.EffectiveEnvironment != "staging" || strings.Contains(v.DSNHint, "publickey") {
		t.Fatalf("view %+v", v)
	}
	// Blank DSN keeps it; no change, no apply.
	changed, err = s.Save(t.Context(), SettingsInput{Environment: "staging"}, "u1")
	if err != nil || changed != nil || st.row.DSN != testDSN || len(live.applied) != 1 {
		t.Fatalf("resave: %v %v %d", changed, err, len(live.applied))
	}
	// Remove clears and applies off.
	changed, err = s.Save(t.Context(), SettingsInput{ClearDSN: true, Environment: "staging"}, "u1")
	if err != nil || strings.Join(changed, ",") != "dsn" || st.row.DSN != "" {
		t.Fatalf("remove: %v %v %+v", changed, err, st.row)
	}
	if last := live.applied[len(live.applied)-1]; last.DSN != "" {
		t.Fatalf("remove applied %+v", last)
	}
}

func TestErrorReportSettings_Validation(t *testing.T) {
	st, live := &memStore{}, &fakeLive{}
	s := &SettingsService{Store: st, Live: live}
	for _, tc := range []struct {
		in    SettingsInput
		field string
	}{
		{SettingsInput{DSN: "not a dsn"}, "dsn"},
		{SettingsInput{DSN: "ftp://k@sentry.example.test/1"}, "dsn"},
		{SettingsInput{DSN: "http://k@sentry.example.test/1"}, "dsn"},
		{SettingsInput{DSN: "https://k:secretkey@sentry.example.test/1"}, "dsn"},
		{SettingsInput{DSN: "https://sentry.example.test/1"}, "dsn"},
		{SettingsInput{DSN: testDSN, Environment: "has space"}, "environment"},
		{SettingsInput{DSN: testDSN, Environment: strings.Repeat("e", 65)}, "environment"},
	} {
		_, err := s.Save(t.Context(), tc.in, "u1")
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("Save(%+v) = %v, want field %s", tc.in, err, tc.field)
		}
		if strings.Contains(err.Error(), "secretkey") || strings.Contains(err.Error(), "k@") {
			t.Errorf("error echoes the DSN: %v", err)
		}
	}
	if st.row != nil || len(live.applied) != 0 {
		t.Fatal("invalid input was stored or applied")
	}
	for _, ok := range []string{"http://k@127.0.0.1:9000/1", "http://k@localhost:9000/1", "http://k@[::1]:9000/1", testDSN} {
		if err := ValidateDSN(ok); err != nil {
			t.Errorf("ValidateDSN(%q) = %v", ok, err)
		}
	}
}

func TestErrorReportSettings_TestUsesFormOrEffectiveAndRefusesOff(t *testing.T) {
	st, live := &memStore{}, &fakeLive{}
	s := &SettingsService{Store: st, Live: live}
	_, err := s.Test(t.Context(), SettingsInput{})
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "dsn" || !strings.Contains(fe.Message, "off") {
		t.Fatalf("test with nothing set = %v", err)
	}
	msg, err := s.Test(t.Context(), SettingsInput{DSN: testDSN})
	if err != nil || !strings.Contains(msg, "https://o1.ingest.sentry.example.test/42") || !strings.Contains(msg, "abc123") || strings.Contains(msg, "publickey") {
		t.Fatalf("test = %q %v", msg, err)
	}
	if st.row != nil || len(live.applied) != 0 {
		t.Fatal("Test saved or applied")
	}
	live.testErr = ErrNotDelivered
	_, err = s.Test(t.Context(), SettingsInput{DSN: testDSN})
	var se *SendError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "not delivered") {
		t.Fatalf("undelivered = %v", err)
	}
}

func TestErrorReportSettings_UnreadableIsOff(t *testing.T) {
	st := &memStore{row: &models.ErrorReportingSettings{DSN: testDSN, Environment: "x"}, unreadable: true}
	s := &SettingsService{Store: st, Live: &fakeLive{}, EnvDSN: testDSN}
	c, src, err := s.Effective(t.Context())
	if err != nil || c.DSN != "" || src != SourceNone {
		t.Fatalf("unreadable effective %+v %q %v", c, src, err)
	}
	v, _ := s.View(t.Context())
	if !v.Unreadable || v.DSNSet {
		t.Fatalf("view %+v", v)
	}
	_, err = s.Test(t.Context(), SettingsInput{})
	var fe *FieldError
	if !errors.As(err, &fe) || !strings.Contains(fe.Message, "ENCRYPTION_KEY") {
		t.Fatalf("test = %v", err)
	}
}
