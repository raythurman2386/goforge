package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func validSettings() BackupConfig {
	return BackupConfig{
		Endpoint: "https://s3.example.com", Region: "auto", Bucket: "bk", Prefix: "goforge/",
		AccessKeyID: "AKIAEXAMPLE", SecretAccessKey: "s3cr3t-value", Schedule: "02:00 America/Chicago",
		RetentionDaily: 7, Timeout: 2 * time.Hour,
	}
}

func TestValidateSettings(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*BackupConfig)
		field string
	}{
		{"valid", func(*BackupConfig) {}, ""},
		{"no bucket", func(b *BackupConfig) { b.Bucket = " " }, "bucket"},
		{"bucket with slash", func(b *BackupConfig) { b.Bucket = "bk/sub" }, "bucket"},
		{"no endpoint", func(b *BackupConfig) { b.Endpoint = "" }, "endpoint"},
		{"endpoint creds", func(b *BackupConfig) { b.Endpoint = "https://user:pw@s3.example.com" }, "endpoint"},
		{"endpoint path", func(b *BackupConfig) { b.Endpoint = "https://s3.example.com/bk" }, "endpoint"},
		{"endpoint scheme", func(b *BackupConfig) { b.Endpoint = "ftp://s3.example.com" }, "endpoint"},
		{"no region", func(b *BackupConfig) { b.Region = "" }, "region"},
		{"no key id", func(b *BackupConfig) { b.AccessKeyID = "" }, "access_key_id"},
		{"no secret", func(b *BackupConfig) { b.SecretAccessKey = "" }, "secret_access_key"},
		{"prefix dotdot", func(b *BackupConfig) { b.Prefix = "a/../b/" }, "prefix"},
		{"retention 0", func(b *BackupConfig) { b.RetentionDaily = 0 }, "retention_daily"},
		{"retention 367", func(b *BackupConfig) { b.RetentionDaily = 367 }, "retention_daily"},
		{"timeout 0", func(b *BackupConfig) { b.Timeout = 0 }, "timeout"},
		{"bad zone", func(b *BackupConfig) { b.Schedule = "02:00 Mars/Base" }, "schedule"},
		{"bad time", func(b *BackupConfig) { b.Schedule = "25:00 UTC" }, "schedule"},
		{"schedule off", func(b *BackupConfig) { b.Schedule = BackupScheduleOff }, ""},
		{"disabled may be empty", func(b *BackupConfig) {
			*b = BackupConfig{Disabled: true, Region: "auto", RetentionDaily: 7, Timeout: time.Hour, Schedule: "off"}
		}, ""},
		{"disabled still checks a bad endpoint", func(b *BackupConfig) { b.Disabled = true; b.Endpoint = "nope://x" }, "endpoint"},
		{"disabled keeps keys optional", func(b *BackupConfig) { b.Disabled = true; b.AccessKeyID, b.SecretAccessKey = "", "" }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := validSettings()
			tc.edit(&b)
			err := b.ValidateSettings()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var fe *BackupFieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("error = %v, want field %s", err, tc.field)
			}
			if strings.Contains(fe.Message, "s3cr3t-value") || strings.Contains(fe.Message, "pw@") {
				t.Errorf("message leaks a value: %s", fe.Message)
			}
		})
	}
}

func TestEnabledHonoursDisabled(t *testing.T) {
	b := validSettings()
	if !b.Enabled() {
		t.Fatal("want enabled")
	}
	b.Disabled = true
	if b.Enabled() {
		t.Fatal("Disabled must turn backups off while keeping the bucket")
	}
}
