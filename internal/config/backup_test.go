package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseDailySchedule(t *testing.T) {
	tests := []struct {
		raw     string
		ok      bool
		wantErr bool
		hour    int
		minute  int
		zone    string
	}{
		{raw: DefaultBackupSchedule, ok: true, hour: 2, minute: 0, zone: "America/Chicago"},
		{raw: " 23:59  UTC ", ok: true, hour: 23, minute: 59, zone: "UTC"},
		{raw: "off", ok: false},
		{raw: "OFF", ok: false},
		{raw: "02:00", wantErr: true},
		{raw: "2:00 UTC", wantErr: true},
		{raw: "24:00 UTC", wantErr: true},
		{raw: "02:60 UTC", wantErr: true},
		{raw: "02:00 Mars/Olympus", wantErr: true},
		{raw: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			s, ok, err := ParseDailySchedule(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && (s.Hour != tt.hour || s.Minute != tt.minute || s.Location.String() != tt.zone) {
				t.Errorf("got %s", s)
			}
		})
	}
}

func TestDailySchedule_Next(t *testing.T) {
	s, _, err := ParseDailySchedule(DefaultBackupSchedule)
	if err != nil {
		t.Fatal(err)
	}
	chi := s.Location
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before today", time.Date(2026, 10, 29, 1, 0, 0, 0, chi), time.Date(2026, 10, 29, 2, 0, 0, 0, chi)},
		{"exactly at", time.Date(2026, 10, 29, 2, 0, 0, 0, chi), time.Date(2026, 10, 30, 2, 0, 0, 0, chi)},
		{"after today", time.Date(2026, 10, 29, 14, 47, 0, 0, chi), time.Date(2026, 10, 30, 2, 0, 0, 0, chi)},
		{"utc input", time.Date(2026, 10, 29, 19, 47, 0, 0, time.UTC), time.Date(2026, 10, 30, 2, 0, 0, 0, chi)},
		// 2027-03-14 02:00 does not exist in Chicago; time.Date moves it to 03:00 CDT.
		{"spring forward", time.Date(2027, 3, 13, 12, 0, 0, 0, chi), time.Date(2027, 3, 14, 3, 0, 0, 0, chi)},
		// 2026-11-01 01:00-02:00 repeats; 02:00 CST happens once.
		{"fall back", time.Date(2026, 10, 31, 12, 0, 0, 0, chi), time.Date(2026, 11, 1, 2, 0, 0, 0, chi)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Next(tt.now)
			if !got.Equal(tt.want) {
				t.Errorf("Next(%s) = %s, want %s", tt.now, got, tt.want)
			}
			if !got.After(tt.now) {
				t.Errorf("Next must be strictly after now")
			}
		})
	}
}

func TestBackupConfig_EndpointHost(t *testing.T) {
	tests := []struct {
		endpoint string
		host     string
		secure   bool
		wantErr  bool
	}{
		{"https://acct.r2.cloudflarestorage.com", "acct.r2.cloudflarestorage.com", true, false},
		{"acct.r2.cloudflarestorage.com", "acct.r2.cloudflarestorage.com", true, false},
		{"http://127.0.0.1:9000", "127.0.0.1:9000", false, false},
		{"http://127.0.0.1:9000/", "127.0.0.1:9000", false, false},
		{"http://user:pass@127.0.0.1:9000", "", false, true},
		{"https://s3.example.com/bucket", "", false, true},
		{"ftp://x", "", false, true},
		{"", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			host, secure, err := BackupConfig{Endpoint: tt.endpoint}.EndpointHost()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (host != tt.host || secure != tt.secure) {
				t.Errorf("got %s secure=%v", host, secure)
			}
			if err != nil && strings.Contains(err.Error(), "pass") {
				t.Errorf("error leaks credentials: %v", err)
			}
		})
	}
}

func TestLoad_BackupDisabledByDefault(t *testing.T) {
	os.Clearenv()
	defer os.Clearenv()
	os.Setenv("DATABASE_URL", "postgres://x@localhost/x")
	os.Setenv("ENCRYPTION_KEY", "k")
	os.Setenv("SESSION_SECRET", "s")
	os.Setenv("CSRF_SECRET", "c")
	// A broken schedule must not matter while the bucket is unset.
	os.Setenv("BACKUP_SCHEDULE", "nonsense")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Backup.Enabled() {
		t.Fatal("backups must be disabled without BACKUP_S3_BUCKET")
	}
	if cfg.Backup.Prefix != DefaultBackupPrefix || cfg.Backup.RetentionDaily != 7 || cfg.Backup.Region != "auto" {
		t.Errorf("defaults = %+v", cfg.Backup)
	}
}

func TestLoad_BackupEnabledValidation(t *testing.T) {
	base := map[string]string{
		"DATABASE_URL":                "postgres://x@localhost/x",
		"ENCRYPTION_KEY":              "k",
		"SESSION_SECRET":              "s",
		"CSRF_SECRET":                 "c",
		"BACKUP_S3_BUCKET":            "goforge-backups",
		"BACKUP_S3_ENDPOINT":          "http://127.0.0.1:9000",
		"BACKUP_S3_ACCESS_KEY_ID":     "id",
		"BACKUP_S3_SECRET_ACCESS_KEY": "topsecret",
		"BACKUP_S3_PREFIX":            "/nightly//",
	}
	tests := []struct {
		name     string
		override map[string]string
		wantErr  string
	}{
		{name: "ok"},
		{name: "missing secret", override: map[string]string{"BACKUP_S3_SECRET_ACCESS_KEY": ""}, wantErr: "BACKUP_S3_SECRET_ACCESS_KEY"},
		{name: "missing endpoint", override: map[string]string{"BACKUP_S3_ENDPOINT": ""}, wantErr: "BACKUP_S3_ENDPOINT"},
		{name: "bad schedule", override: map[string]string{"BACKUP_SCHEDULE": "2am"}, wantErr: "BACKUP_SCHEDULE"},
		{name: "zero retention", override: map[string]string{"BACKUP_RETENTION_DAILY": "0"}, wantErr: "BACKUP_RETENTION_DAILY"},
		{name: "dotdot prefix", override: map[string]string{"BACKUP_S3_PREFIX": "a/../b"}, wantErr: "BACKUP_S3_PREFIX"},
		{name: "schedule off", override: map[string]string{"BACKUP_SCHEDULE": "off"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			defer os.Clearenv()
			for k, v := range base {
				os.Setenv(k, v)
			}
			for k, v := range tt.override {
				os.Setenv(k, v)
			}
			cfg, err := Load()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want mention of %s", err, tt.wantErr)
				}
				if strings.Contains(err.Error(), "topsecret") {
					t.Fatalf("error leaks the secret: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Backup.Prefix != "nightly/" {
				t.Errorf("prefix = %q, want nightly/", cfg.Backup.Prefix)
			}
		})
	}
}
