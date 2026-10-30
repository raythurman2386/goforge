package backup

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/config"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

type memSettings struct {
	mu      sync.Mutex
	row     *models.BackupSettings
	upserts int
	getErr  error
}

func (m *memSettings) Get(context.Context) (*models.BackupSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.row == nil {
		return nil, repositories.ErrNotFound
	}
	cp := *m.row
	return &cp, nil
}

func (m *memSettings) Upsert(_ context.Context, s *models.BackupSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	cp.UpdatedAt = time.Now()
	m.row = &cp
	m.upserts++
	return nil
}

func (m *memSettings) InsertIfAbsent(ctx context.Context, s *models.BackupSettings) (bool, error) {
	m.mu.Lock()
	if m.row != nil {
		m.mu.Unlock()
		return false, nil
	}
	m.mu.Unlock()
	return true, m.Upsert(ctx, s)
}

type applied struct {
	mu   sync.Mutex
	cfgs []config.BackupConfig
	srcs []string
}

func (a *applied) Apply(cfg config.BackupConfig, source string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfgs = append(a.cfgs, cfg)
	a.srcs = append(a.srcs, source)
}

const (
	envKeyID  = "ENVKEYID-0000-AAAA"
	envSecret = "ENV-SECRET-VALUE-1111" //nolint:gosec // test fixture, not a credential
)

func envBackup() config.BackupConfig {
	return config.BackupConfig{
		Endpoint: "https://env.example.com", Region: "auto", Bucket: "env-bucket", Prefix: "goforge/",
		AccessKeyID: envKeyID, SecretAccessKey: envSecret, Schedule: "02:00 America/Chicago",
		RetentionDaily: 7, Timeout: 2 * time.Hour,
	}
}

func formFrom(c config.BackupConfig) SettingsInput {
	in := SettingsInput{
		Enabled: c.Enabled(), Endpoint: c.Endpoint, Region: c.Region, Bucket: c.Bucket, Prefix: c.Prefix,
		ForcePathStyle: c.ForcePathStyle, RetentionDaily: "7", Timeout: "2h",
	}
	f := strings.Fields(c.Schedule)
	in.ScheduleTime, in.ScheduleZone = f[0], f[1]
	return in
}

func TestEffective_PrecedenceA(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	env := envBackup()

	got, src, err := Effective(ctx, store, env)
	if err != nil || src != SourceEnvironment || got.Bucket != "env-bucket" {
		t.Fatalf("no row: %v %s %+v", err, src, got)
	}

	row := SettingsFromConfig(env)
	row.Bucket, row.Enabled = "db-bucket", true
	store.row = row
	got, src, err = Effective(ctx, store, env)
	if err != nil || src != SourceSettings || got.Bucket != "db-bucket" {
		t.Fatalf("row wins: %v %s %+v", err, src, got)
	}

	// A disabled row still wins: env does not turn backups back on.
	store.row.Enabled = false
	got, _, _ = Effective(ctx, store, env)
	if got.Enabled() || got.Bucket != "db-bucket" {
		t.Fatalf("disabled row: %+v", got)
	}

	store.getErr = repositories.ErrBackupSettingsUnreadable
	if _, _, err := Effective(ctx, store, env); !errors.Is(err, repositories.ErrBackupSettingsUnreadable) {
		t.Fatalf("unreadable row: %v", err)
	}
}

func TestAutoImport(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	if ok, err := AutoImport(ctx, store, config.BackupConfig{}); ok || err != nil || store.row != nil {
		t.Fatalf("no bucket must not import: %v %v", ok, err)
	}
	ok, err := AutoImport(ctx, store, envBackup())
	if !ok || err != nil || store.row == nil || !store.row.Enabled || store.row.SecretAccessKey != envSecret {
		t.Fatalf("first start: %v %v %+v", ok, err, store.row)
	}
	store.row.Bucket = "admin-changed"
	if ok, _ := AutoImport(ctx, store, envBackup()); ok || store.row.Bucket != "admin-changed" {
		t.Fatal("import must not overwrite an existing row")
	}
}

func newSvc(store *memSettings) (*SettingsService, *applied) {
	a := &applied{}
	return &SettingsService{Store: store, Env: envBackup(), Live: a}, a
}

func TestSave_BlankKeepsAndRotates(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	svc, live := newSvc(store)

	// First save from env: blank keys keep the env keys.
	in := formFrom(envBackup())
	in.Bucket = "new-bucket"
	changed, err := svc.Save(ctx, in, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "bucket" {
		t.Errorf("changed = %v", changed)
	}
	if store.row.AccessKeyID != envKeyID || store.row.SecretAccessKey != envSecret || store.row.Bucket != "new-bucket" {
		t.Fatalf("blank must keep keys: %+v", store.row)
	}
	if !store.row.UpdatedBy.Valid || store.row.UpdatedBy.String != "u1" {
		t.Error("updated_by not set")
	}
	if len(live.cfgs) != 1 || live.srcs[0] != SourceSettings || live.cfgs[0].Bucket != "new-bucket" {
		t.Fatalf("not applied live: %+v", live)
	}

	// Rotate the secret only.
	in.SecretAccessKey = "ROTATED-SECRET-2222"
	changed, err = svc.Save(ctx, in, "u1")
	if err != nil || strings.Join(changed, ",") != "secret_access_key" {
		t.Fatalf("rotate: %v %v", changed, err)
	}
	if store.row.SecretAccessKey != "ROTATED-SECRET-2222" || store.row.AccessKeyID != envKeyID {
		t.Fatal("rotate did not store the new secret")
	}

	// Saving the same form again (blank secret) changes nothing.
	in.SecretAccessKey = ""
	before := store.upserts
	changed, err = svc.Save(ctx, in, "u1")
	if err != nil || len(changed) != 0 || store.upserts != before {
		t.Fatalf("no-op save: %v %v upserts %d->%d", changed, err, before, store.upserts)
	}
}

func TestSave_DisableKeepsSettings(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	svc, live := newSvc(store)
	in := formFrom(envBackup())
	in.Enabled = false
	changed, err := svc.Save(ctx, in, "")
	if err != nil || strings.Join(changed, ",") != "enabled" {
		t.Fatalf("disable: %v %v", changed, err)
	}
	if store.row.Enabled || store.row.Bucket != "env-bucket" || store.row.SecretAccessKey != envSecret {
		t.Fatalf("disable must keep the destination: %+v", store.row)
	}
	if live.cfgs[0].Enabled() {
		t.Fatal("applied config still enabled")
	}
	v, err := svc.View(ctx)
	if err != nil || v.Enabled || v.Bucket != "env-bucket" || !v.SecretSet || v.Source != SourceSettings {
		t.Fatalf("view after disable: %+v %v", v, err)
	}
}

func TestSave_ValidationStoresNothing(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	svc, live := newSvc(store)
	for name, edit := range map[string]func(*SettingsInput){
		"retention":  func(in *SettingsInput) { in.RetentionDaily = "0" },
		"retention?": func(in *SettingsInput) { in.RetentionDaily = "seven" },
		"timeout":    func(in *SettingsInput) { in.Timeout = "soon" },
		"zone":       func(in *SettingsInput) { in.ScheduleZone = "Nowhere/Land" },
		"endpoint":   func(in *SettingsInput) { in.Endpoint = "https://k:" + envSecret + "@x.example.com" },
		"clear":      func(in *SettingsInput) { in.ClearCredentials = true },
	} {
		in := formFrom(envBackup())
		edit(&in)
		_, err := svc.Save(ctx, in, "")
		var fe *config.BackupFieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), envSecret) {
			t.Errorf("%s: error leaks the secret", name)
		}
	}
	if store.row != nil || len(live.cfgs) != 0 {
		t.Fatal("a failed save stored or applied settings")
	}
}

func TestSave_ClearCredentialsWhenDisabled(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	svc, _ := newSvc(store)
	in := formFrom(envBackup())
	in.Enabled, in.ClearCredentials = false, true
	changed, err := svc.Save(ctx, in, "")
	if err != nil {
		t.Fatal(err)
	}
	if store.row.AccessKeyID != "" || store.row.SecretAccessKey != "" {
		t.Fatal("keys not cleared")
	}
	if got := strings.Join(changed, ","); got != "enabled,access_key_id,secret_access_key" {
		t.Errorf("changed = %s", got)
	}
}

func TestView_HidesSecret(t *testing.T) {
	v := NewSettingsView(envBackup(), SourceEnvironment)
	if !v.SecretSet || !v.AccessKeySet || v.AccessKeyHint != "AAAA" {
		t.Fatalf("view = %+v", v)
	}
	if v.ScheduleTime != "02:00" || v.ScheduleZone != "America/Chicago" || v.Timeout != "2h" {
		t.Errorf("view schedule = %+v", v)
	}
	short := envBackup()
	short.AccessKeyID = "SHORTKEY"
	if NewSettingsView(short, SourceEnvironment).AccessKeyHint != "" {
		t.Error("short key ids must not show a hint")
	}
}

func TestTest_UsesStoredKeysAndRedacts(t *testing.T) {
	ctx := context.Background()
	store := &memSettings{}
	svc, live := newSvc(store)
	var probed config.BackupConfig
	svc.Probe = func(_ context.Context, c config.BackupConfig) (bool, error) {
		probed = c
		return false, errors.New("403 for key " + c.AccessKeyID + " secret " + c.SecretAccessKey)
	}
	svc.Redactor = func(extra ...config.BackupConfig) *Redactor {
		return RedactorFor(&config.Config{EncryptionKey: "app-encryption-key"}, extra...)
	}
	in := formFrom(envBackup())
	in.Enabled = false // Test always checks the destination
	_, err := svc.Test(ctx, in)
	var pe *ProbeError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v", err)
	}
	if probed.SecretAccessKey != envSecret || probed.AccessKeyID != envKeyID {
		t.Fatal("blank form must test the stored keys")
	}
	if strings.Contains(pe.Message, envSecret) || strings.Contains(pe.Message, envKeyID) {
		t.Fatalf("probe error leaks: %s", pe.Message)
	}
	if store.row != nil || len(live.cfgs) != 0 {
		t.Fatal("Test must not store or apply")
	}

	svc.Probe = func(context.Context, config.BackupConfig) (bool, error) { return true, nil }
	msg, err := svc.Test(ctx, in)
	if err != nil || !strings.Contains(msg, "env-bucket/goforge/") {
		t.Fatalf("ok probe: %q %v", msg, err)
	}
}

func TestRedactorFor_CoversSettingsKeys(t *testing.T) {
	app := &config.Config{EncryptionKey: "app-encryption-key", Backup: envBackup()}
	db := envBackup()
	db.AccessKeyID, db.SecretAccessKey = "DBKEYID-9999", "DB-ONLY-SECRET-3333"
	r := RedactorFor(app, db)
	out := r.Redact("env " + envSecret + " db " + db.SecretAccessKey + " id " + db.AccessKeyID)
	for _, s := range []string{envSecret, db.SecretAccessKey, db.AccessKeyID} {
		if strings.Contains(out, s) {
			t.Errorf("not redacted: %s in %s", s, out)
		}
	}
}

func TestChangedFields_NamesOnly(t *testing.T) {
	a, b := envBackup(), envBackup()
	b.SecretAccessKey, b.RetentionDaily, b.ForcePathStyle = "x", 9, true
	if got := strings.Join(ChangedFields(a, b), ","); got != "secret_access_key,force_path_style,retention_daily" {
		t.Errorf("changed = %s", got)
	}
}
