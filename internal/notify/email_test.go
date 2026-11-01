package notify_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/notify/notifytest"
)

// Throwaway SMTP credentials; not real.
const (
	smtpUser = "alerts@example.test"
	smtpPass = "smtp-pass-DO-NOT-LEAK" //nolint:gosec // throwaway test value
)

func smtpRow(srv *notifytest.SMTPServer, mode, user, pass string) *models.NotifySettings {
	return &models.NotifySettings{Enabled: true, Kind: models.NotifyKindEmail, SMTP: models.SMTPSettings{
		Host: srv.Host, Port: srv.Port, TLS: mode, Username: user, Password: pass,
		From: "GoForge <goforge@example.test>", To: "ops@example.test, Raymond <me@example.test>",
	}}
}

func emailService(t *testing.T, srv *notifytest.SMTPServer, row *models.NotifySettings) (*notify.Service, *bytes.Buffer) {
	t.Helper()
	st := &notifytest.Store{}
	if err := st.Upsert(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	return &notify.Service{
		Store: st, BaseURL: "https://forge.example.test", Redactor: notify.NewRedactor(),
		SMTPTLS: srv.ClientTLS, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}, &logs
}

func TestEmailSendsOneMessagePerTLSMode(t *testing.T) {
	for _, mode := range []string{models.SMTPTLSStartTLS, models.SMTPTLSImplicit, models.SMTPTLSNone} {
		t.Run(mode, func(t *testing.T) {
			srv := notifytest.NewSMTPServer(t, mode, smtpUser, smtpPass)
			svc, _ := emailService(t, srv, smtpRow(srv, mode, smtpUser, smtpPass))
			ev := failureEvents()[0]
			svc.Notify(context.Background(), ev)
			svc.Wait()
			msgs := srv.Messages()
			if len(msgs) != 1 {
				t.Fatalf("got %d messages, want 1", len(msgs))
			}
			m := msgs[0]
			if m.TLS != (mode != models.SMTPTLSNone) || m.AuthUser != smtpUser {
				t.Errorf("tls=%v auth=%q", m.TLS, m.AuthUser)
			}
			if m.From != "goforge@example.test" || strings.Join(m.To, ",") != "ops@example.test,me@example.test" {
				t.Errorf("envelope from=%q to=%v", m.From, m.To)
			}
			for _, want := range []string{
				"Subject: GoForge: Deployment failed: shop / production", "From: \"GoForge\" <goforge@example.test>",
				"X-GoForge-Event: deploy.failed", "build failed", "Resource: deployment dep-1",
				"Link: https://forge.example.test/projects/p1/deployments/dep-1", "Audit ID: aud-1",
			} {
				if !strings.Contains(m.Data, want) {
					t.Errorf("message missing %q:\n%s", want, m.Data)
				}
			}
			assertNoSecrets(t, m.Data, smtpPass)
		})
	}
}

func TestEmailBodyIsRedacted(t *testing.T) {
	srv := notifytest.NewSMTPServer(t, models.SMTPTLSStartTLS, "", "")
	svc, _ := emailService(t, srv, smtpRow(srv, models.SMTPTLSStartTLS, "", ""))
	svc.Redactor = notify.NewRedactor(knownKey)
	svc.Notify(context.Background(), notify.Event{Kind: notify.EventBackupFailed, Resource: notify.Resource{Type: "backup_run", ID: "r"}, Error: leakyError()})
	svc.Wait()
	msgs := srv.Messages()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages", len(msgs))
	}
	assertNoSecrets(t, msgs[0].Data, dbPass, knownKey, "abc.def.ghi")
}

// The fake answers a bad AUTH with the submitted password in its reply. The
// Test error, and the Notify log line, must carry neither.
func TestEmailAuthFailureNeverLeaksThePassword(t *testing.T) {
	srv := notifytest.NewSMTPServer(t, models.SMTPTLSStartTLS, smtpUser, "the-right-one")
	row := smtpRow(srv, models.SMTPTLSStartTLS, smtpUser, smtpPass)

	svc, logs := emailService(t, srv, row)
	svc.Notify(context.Background(), failureEvents()[0])
	svc.Wait()
	if !strings.Contains(logs.String(), "notification not delivered") || !strings.Contains(logs.String(), "rejected the username or password") {
		t.Errorf("log: %s", logs.String())
	}
	assertNoSecrets(t, logs.String(), smtpPass, "got password")

	st := &notifytest.Store{}
	_ = st.Upsert(context.Background(), row)
	s := &notify.SettingsService{Store: st, Redactor: notify.NewRedactor(), SMTPTLS: srv.ClientTLS}
	_, err := s.Test(context.Background(), notify.SettingsInput{
		Kind: models.NotifyKindEmail, SMTPHost: srv.Host, SMTPPort: strconv.Itoa(srv.Port), SMTPTLS: models.SMTPTLSStartTLS,
		SMTPUsername: smtpUser, SMTPFrom: row.SMTP.From, SMTPTo: row.SMTP.To,
	})
	var se *notify.SendError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "rejected the username or password") {
		t.Fatalf("Test = %v, want auth SendError", err)
	}
	assertNoSecrets(t, se.Message, smtpPass, "got password", smtpUser)
	if len(srv.Messages()) != 0 {
		t.Error("a message was accepted without auth")
	}
}

func TestEmailUntrustedCertificateAndNoSTARTTLS(t *testing.T) {
	srv := notifytest.NewSMTPServer(t, models.SMTPTLSStartTLS, "", "")
	svc, logs := emailService(t, srv, smtpRow(srv, models.SMTPTLSStartTLS, "", ""))
	svc.SMTPTLS = nil // system roots do not trust the fake
	svc.Notify(context.Background(), failureEvents()[0])
	svc.Wait()
	if !strings.Contains(logs.String(), "is not trusted") || len(srv.Messages()) != 0 {
		t.Errorf("untrusted cert: log %s, %d messages", logs.String(), len(srv.Messages()))
	}

	plain := notifytest.NewSMTPServer(t, models.SMTPTLSNone, "", "")
	svc2, logs2 := emailService(t, plain, smtpRow(plain, models.SMTPTLSStartTLS, "", ""))
	svc2.Notify(context.Background(), failureEvents()[0])
	svc2.Wait()
	if !strings.Contains(logs2.String(), "does not offer STARTTLS") || len(plain.Messages()) != 0 {
		t.Errorf("no STARTTLS: log %s", logs2.String())
	}
}

func TestEmailSettingsSaveTestAndKindSwitch(t *testing.T) {
	srv := notifytest.NewSMTPServer(t, models.SMTPTLSStartTLS, smtpUser, smtpPass)
	s, rcv, st := newSettings(t)
	s.SMTPTLS = srv.ClientTLS
	ctx := context.Background()

	// A webhook first; switching to email must keep its URL and token.
	if _, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook", URL: rcv.URL + topicPath, Token: token}, ""); err != nil {
		t.Fatal(err)
	}
	in := notify.SettingsInput{
		Enabled: true, Kind: models.NotifyKindEmail, SMTPHost: srv.Host, SMTPPort: strconv.Itoa(srv.Port),
		SMTPTLS: models.SMTPTLSStartTLS, SMTPUsername: smtpUser, SMTPPassword: smtpPass,
		SMTPFrom: "goforge@example.test", SMTPTo: "ops@example.test",
	}
	changed, err := s.Save(ctx, in, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(changed, ","); got != "kind,smtp_host,smtp_port,smtp_tls,smtp_username,smtp_password,smtp_from,smtp_to" {
		t.Errorf("changed = %s", got)
	}
	row := st.Row()
	if row.URL != rcv.URL+topicPath || row.Token != token || row.SMTP.Password != smtpPass || row.SMTP.TLS != models.SMTPTLSStartTLS {
		t.Errorf("row after switch: url/token kept=%v pw=%v tls=%q", row.URL != "" && row.Token != "", row.SMTP.Password != "", row.SMTP.TLS)
	}

	// The view never holds the password.
	v, _ := s.View(ctx)
	if !v.SMTPPasswordSet || !v.Configured || v.SMTPHost != srv.Host || v.Kind != models.NotifyKindEmail {
		t.Errorf("view %+v", v)
	}

	// Blank password keeps it; Test uses it and saves nothing.
	in.SMTPPassword = ""
	if changed, err = s.Save(ctx, in, ""); err != nil || changed != nil {
		t.Errorf("blank password save = %v %v, want no changes", changed, err)
	}
	msg, err := s.Test(ctx, in)
	if err != nil || !strings.Contains(msg, "Test email sent via "+srv.Addr()+" to 1 recipient(s)") {
		t.Fatalf("Test = %q %v", msg, err)
	}
	if m := srv.Messages(); len(m) != 1 || !strings.Contains(m[0].Data, "X-GoForge-Event: test") || m[0].AuthUser != smtpUser {
		t.Errorf("test message: %+v", m)
	}
	assertNoSecrets(t, msg, smtpPass)

	// A new host drops the stored password unless one is typed.
	in.SMTPHost = "localhost"
	if changed, err = s.Save(ctx, in, ""); err != nil || st.Row().SMTP.Password != "" || !strings.Contains(strings.Join(changed, ","), "smtp_password") {
		t.Errorf("host move: changed=%v err=%v pw kept=%v", changed, err, st.Row().SMTP.Password != "")
	}

	// Back to webhook: the SMTP fields stay, and the webhook still works.
	if _, err = s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook"}, ""); err != nil || st.Row().SMTP.Host != "localhost" {
		t.Fatalf("back to webhook: %v", err)
	}
	if _, err = s.Test(ctx, notify.SettingsInput{Kind: "webhook"}); err != nil || len(rcv.Requests()) != 1 {
		t.Errorf("webhook test after email: %v, %d requests", err, len(rcv.Requests()))
	}
}

func TestEmailSettingsValidation(t *testing.T) {
	s, _, st := newSettings(t)
	ctx := context.Background()
	ok := notify.SettingsInput{Kind: "email", SMTPHost: "smtp.example.test", SMTPFrom: "a@example.test", SMTPTo: "b@example.test"}
	with := func(f func(*notify.SettingsInput)) notify.SettingsInput { in := ok; f(&in); return in }
	cases := []struct {
		in    notify.SettingsInput
		field string
	}{
		{with(func(i *notify.SettingsInput) { i.SMTPTLS = "ssl" }), "smtp_tls"},
		{with(func(i *notify.SettingsInput) { i.SMTPHost = "smtp://smtp.example.test" }), "smtp_host"},
		{with(func(i *notify.SettingsInput) { i.SMTPHost = "smtp.example.test:587" }), "smtp_host"},
		{with(func(i *notify.SettingsInput) { i.SMTPPort = "70000" }), "smtp_port"},
		{with(func(i *notify.SettingsInput) { i.SMTPPort = "x" }), "smtp_port"},
		{with(func(i *notify.SettingsInput) { i.SMTPFrom = "not an address" }), "smtp_from"},
		{with(func(i *notify.SettingsInput) { i.SMTPTo = "b@example.test; c@example.test" }), "smtp_to"},
		{with(func(i *notify.SettingsInput) { i.SMTPTo = strings.Repeat("x@example.test,", 10) + "y@example.test" }), "smtp_to"},
		{with(func(i *notify.SettingsInput) { i.SMTPTLS = "none"; i.SMTPUsername = "u" }), "smtp_tls"},
		{with(func(i *notify.SettingsInput) { i.Enabled = true; i.SMTPTo = "" }), "smtp_to"},
		{with(func(i *notify.SettingsInput) { i.Enabled = true; i.SMTPHost = "" }), "smtp_host"},
	}
	for _, c := range cases {
		_, err := s.Save(ctx, c.in, "")
		var fe *notify.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("Save(%+v) = %v, want field %s", c.in, err, c.field)
		}
	}
	if st.Row() != nil {
		t.Error("an invalid form was stored")
	}
	// none without a username, or with one on localhost, is allowed; the
	// port defaults from the TLS mode.
	if _, err := s.Save(ctx, with(func(i *notify.SettingsInput) { i.SMTPTLS = "none"; i.SMTPHost = "127.0.0.1"; i.SMTPUsername = "u" }), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(ctx, with(func(i *notify.SettingsInput) { i.SMTPTLS = "implicit" }), ""); err != nil || st.Row().SMTP.Port != 465 {
		t.Errorf("tls default port: %v %d", err, st.Row().SMTP.Port)
	}
}
