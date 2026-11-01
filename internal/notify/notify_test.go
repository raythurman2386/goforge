package notify_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/notify/notifytest"
)

// Throwaway values only; none of these is a real credential.
const (
	topicPath = "/goforge-alerts-s3cr3tTopic"
	token     = "tk_notifier_token_DO_NOT_LEAK" //nolint:gosec // throwaway test value
	dbPass    = "pg-pass-DO-NOT-LEAK"           //nolint:gosec // throwaway test value
	knownKey  = "AKIA-KNOWN-SECRET-DO-NOT-LEAK"
)

// leakyError is an error message that carries every kind of secret the
// redactor must remove.
func leakyError() string {
	return "pg_dump: connection to postgres://goforge:" + dbPass + "@db:5432/goforge failed; " +
		"password=" + dbPass + " api_key=" + knownKey + " Authorization: Bearer abc.def.ghi " + knownKey
}

func assertNoSecrets(t *testing.T, raw string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(raw, s) {
			t.Errorf("request contains secret %q:\n%s", s, raw)
		}
	}
}

func failureEvents() []notify.Event {
	return []notify.Event{
		{Kind: notify.EventDeployFailed, Resource: notify.Resource{Type: "deployment", ID: "dep-1", Name: "shop / production"}, Error: "build failed", Path: "/projects/p1/deployments/dep-1", AuditID: "aud-1"},
		{Kind: notify.EventServiceFailed, Resource: notify.Resource{Type: "service_instance", ID: "svc-1", Name: "pg"}, Error: "container exited with code 1", Path: "/services/svc-1", AuditID: "aud-2"},
		{Kind: notify.EventBackupFailed, Resource: notify.Resource{Type: "backup_run", ID: "run-1", Name: "2026/11/01"}, Error: "upload failed", Path: "/settings/profile#backup-status"},
	}
}

func TestWebhookOnePOSTPerFailureType(t *testing.T) {
	svc, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, topicPath, token)
	for _, ev := range failureEvents() {
		svc.Notify(context.Background(), ev)
		svc.Wait()
	}
	reqs := rcv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("got %d POSTs, want 3", len(reqs))
	}
	for i, ev := range failureEvents() {
		r := reqs[i]
		if r.Method != http.MethodPost || r.Path != topicPath {
			t.Errorf("request %d: %s %s", i, r.Method, r.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Errorf("authorization header missing")
		}
		p := r.Payload(t)
		if p.Source != "goforge" || p.Event != ev.Kind || p.Resource != ev.Resource || p.Error != ev.Error || p.AuditID != ev.AuditID {
			t.Errorf("payload %d = %+v, want event %+v", i, p, ev)
		}
		if p.Link != "https://forge.example.test"+ev.Path {
			t.Errorf("link %q", p.Link)
		}
		if _, err := time.Parse(time.RFC3339, p.Time); err != nil {
			t.Errorf("time %q: %v", p.Time, err)
		}
		// The body never holds the notifier's own URL or token.
		assertNoSecrets(t, r.Body, topicPath, token)
	}
}

func TestNtfyOnePOSTPerFailureType(t *testing.T) {
	svc, rcv, _ := notifytest.Setup(t, models.NotifyKindNtfy, topicPath, "")
	for _, ev := range failureEvents() {
		svc.Notify(context.Background(), ev)
		svc.Wait()
	}
	reqs := rcv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("got %d POSTs, want 3", len(reqs))
	}
	r := reqs[0]
	if r.Header.Get("Title") != "GoForge: Deployment failed: shop / production" {
		t.Errorf("Title %q", r.Header.Get("Title"))
	}
	if r.Header.Get("Priority") != "high" || r.Header.Get("Click") != "https://forge.example.test/projects/p1/deployments/dep-1" {
		t.Errorf("headers %v", r.Header)
	}
	if r.Header.Get("Authorization") != "" {
		t.Error("no token configured, but Authorization was sent")
	}
	if !strings.Contains(r.Body, "build failed") || !strings.Contains(r.Body, "deployment dep-1") || !strings.Contains(r.Body, "audit_id aud-1") {
		t.Errorf("body %q", r.Body)
	}
	if !strings.HasPrefix(reqs[1].Header.Get("Title"), "GoForge: Service failed") || !strings.HasPrefix(reqs[2].Header.Get("Title"), "GoForge: Backup failed") {
		t.Errorf("titles %q %q", reqs[1].Header.Get("Title"), reqs[2].Header.Get("Title"))
	}
}

func TestPayloadRedactsSecrets(t *testing.T) {
	for _, kind := range []string{models.NotifyKindWebhook, models.NotifyKindNtfy} {
		t.Run(kind, func(t *testing.T) {
			svc, rcv, _ := notifytest.Setup(t, kind, topicPath, token)
			svc.Redactor = notify.NewRedactor(knownKey)
			svc.Notify(context.Background(), notify.Event{Kind: notify.EventBackupFailed, Resource: notify.Resource{Type: "backup_run", ID: "r"}, Error: leakyError()})
			svc.Wait()
			reqs := rcv.Requests()
			if len(reqs) != 1 {
				t.Fatalf("got %d POSTs", len(reqs))
			}
			// The request goes to the topic with the token as its credential;
			// apart from that, neither appears, and nor does any secret from
			// the error.
			r := reqs[0]
			r.Header.Del("Authorization")
			r.Path = ""
			raw := r.Raw()
			assertNoSecrets(t, raw, dbPass, knownKey, "abc.def.ghi", token, topicPath)
			if !strings.Contains(raw, "pg_dump: connection to") || !strings.Contains(raw, "[redacted]") {
				t.Errorf("redacted error lost its context: %s", raw)
			}
		})
	}
}

func TestRedactor(t *testing.T) {
	r := notify.NewRedactor("hunter2-known").WithFunc(func(s string) string { return strings.ReplaceAll(s, "custom-secret", "[X]") })
	cases := map[string][]string{
		"postgres://u:p4ss@h/db":           {"p4ss"},
		"redis://:onlypw@cache:6379":       {"onlypw"},
		"PASSWORD=abc123 next":             {"abc123"},
		"secret_key: zzz999":               {"zzz999"},
		"token=\"quoted-tok\"":             {"quoted-tok"},
		"auth: Basic dXNlcjpwYXNz":         {"dXNlcjpwYXNz"},
		"value hunter2-known in log":       {"hunter2-known"},
		"custom-secret here":               {"custom-secret"},
		"x-access_key=AKIAXYZ, other=fine": {"AKIAXYZ"},
	}
	for in, gone := range cases {
		out := r.Redact(in)
		for _, g := range gone {
			if strings.Contains(out, g) {
				t.Errorf("Redact(%q) = %q, still has %q", in, out, g)
			}
		}
	}
	if got := r.Redact("plain error, no secrets"); got != "plain error, no secrets" {
		t.Errorf("plain text changed: %q", got)
	}
	if got := r.Redact(strings.Repeat("a", 2000)); len(got) > 520 {
		t.Errorf("not capped: %d bytes", len(got))
	}
}

func TestNotifyOffDisabledOrMissing(t *testing.T) {
	svc, rcv, st := notifytest.Setup(t, models.NotifyKindWebhook, topicPath, "")
	row := st.Row()
	row.Enabled = false
	_ = st.Upsert(context.Background(), row)
	svc.Notify(context.Background(), failureEvents()[0])
	svc.Wait()
	if svc.Enabled(context.Background()) {
		t.Error("Enabled with enabled=false")
	}
	empty := &notify.Service{Store: &notifytest.Store{}, Client: rcv.Client()}
	empty.Notify(context.Background(), failureEvents()[0])
	empty.Wait()
	unreadable := &notify.Service{Store: &notifytest.Store{GetErr: repositories.ErrNotifySettingsUnreadable}, Client: rcv.Client()}
	unreadable.Notify(context.Background(), failureEvents()[0])
	unreadable.Wait()
	var nilSvc *notify.Service
	nilSvc.Notify(context.Background(), failureEvents()[0])
	nilSvc.Wait()
	if n := len(rcv.Requests()); n != 0 {
		t.Fatalf("got %d POSTs while off", n)
	}
}

func TestNotifyCanceledContextStillSends(t *testing.T) {
	svc, rcv, _ := notifytest.Setup(t, models.NotifyKindWebhook, topicPath, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.Notify(ctx, failureEvents()[2])
	svc.Wait()
	if n := len(rcv.Requests()); n != 1 {
		t.Fatalf("got %d POSTs, want 1 (a backup canceled on shutdown still alerts)", n)
	}
}

func newSettings(t *testing.T) (*notify.SettingsService, *notifytest.Receiver, *notifytest.Store) {
	t.Helper()
	rcv := notifytest.NewReceiver(t)
	st := &notifytest.Store{}
	return &notify.SettingsService{Store: st, BaseURL: "https://forge.example.test", Redactor: notify.NewRedactor(), Client: rcv.Client()}, rcv, st
}

func TestSettingsSaveKeepsBlankSecretsAndReportsFieldNames(t *testing.T) {
	s, rcv, st := newSettings(t)
	ctx := context.Background()
	v, err := s.View(ctx)
	if err != nil || v.Saved || v.Enabled || v.Kind != models.NotifyKindNtfy {
		t.Fatalf("initial view %+v %v", v, err)
	}
	changed, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook", URL: rcv.URL + topicPath, Token: token}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, ",") != "enabled,kind,url,token" {
		t.Errorf("changed = %v", changed)
	}
	// Blank URL and token keep the stored ones.
	changed, err = s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook"}, "")
	if err != nil || len(changed) != 0 {
		t.Fatalf("resave: %v %v", changed, err)
	}
	if r := st.Row(); r.URL != rcv.URL+topicPath || r.Token != token {
		t.Fatal("blank fields cleared the stored values")
	}
	v, _ = s.View(ctx)
	if !v.Enabled || !v.URLSet || !v.TokenSet || v.URLHost != rcv.URL {
		t.Errorf("view %+v", v)
	}
	if strings.Contains(v.URLHost, topicPath) {
		t.Error("view leaks the URL path")
	}
	changed, _ = s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook", ClearToken: true}, "")
	if strings.Join(changed, ",") != "token" || st.Row().Token != "" {
		t.Errorf("clear token: %v", changed)
	}
}

// The saved token only goes to the host it was entered for.
func TestSettingsTokenStaysWithItsHost(t *testing.T) {
	s, rcv, st := newSettings(t)
	ctx := context.Background()
	if _, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook", URL: rcv.URL + topicPath, Token: token}, ""); err != nil {
		t.Fatal(err)
	}
	// Same host, new path: the token is kept.
	if _, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook", URL: rcv.URL + "/other"}, ""); err != nil || st.Row().Token != token {
		t.Fatalf("same host dropped the token: %v", err)
	}
	// Test against another host sends no Authorization header and stores nothing.
	other := notifytest.NewReceiver(t)
	addr := other.Listener.Addr().String()
	s.Client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	if _, err := s.Test(ctx, notify.SettingsInput{Kind: "webhook", URL: "http://elsewhere.example.test/hook"}); err != nil {
		t.Fatal(err)
	}
	if reqs := other.Requests(); len(reqs) != 1 || reqs[0].Header.Get("Authorization") != "" {
		t.Fatalf("token sent to another host: %+v", reqs)
	}
	if st.Row().Token != token {
		t.Fatal("Test changed the stored token")
	}
	// Saving another host without a token drops the saved one.
	changed, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "webhook", URL: "http://elsewhere.example.test/hook"}, "")
	if err != nil || strings.Join(changed, ",") != "url,token" || st.Row().Token != "" {
		t.Fatalf("host change kept the token: %v %v", changed, err)
	}
}

func TestSettingsValidation(t *testing.T) {
	s, _, st := newSettings(t)
	ctx := context.Background()
	cases := []struct {
		in    notify.SettingsInput
		field string
	}{
		{notify.SettingsInput{Enabled: true, Kind: "ntfy"}, "url"},
		{notify.SettingsInput{Kind: "sms", URL: "https://ntfy.sh/x"}, "kind"},
		{notify.SettingsInput{Kind: "ntfy", URL: "ftp://ntfy.sh/x"}, "url"},
		{notify.SettingsInput{Kind: "ntfy", URL: "ntfy.sh/x"}, "url"},
		{notify.SettingsInput{Kind: "ntfy", URL: "https://user:pw@ntfy.sh/x"}, "url"},
		{notify.SettingsInput{Kind: "ntfy", URL: "https://ntfy.sh/x#frag"}, "url"},
	}
	for _, c := range cases {
		_, err := s.Save(ctx, c.in, "")
		var fe *notify.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("Save(%+v) = %v, want field %s", c.in, err, c.field)
		}
		if strings.Contains(err.Error(), "pw@") {
			t.Errorf("error echoes the URL: %v", err)
		}
	}
	if st.Row() != nil {
		t.Error("invalid input was stored")
	}
	// Off with no URL is a valid save.
	if _, err := s.Save(ctx, notify.SettingsInput{Kind: "ntfy"}, ""); err != nil {
		t.Errorf("off save: %v", err)
	}
}

func TestSettingsUnreadableNeedsURLAgain(t *testing.T) {
	s, rcv, st := newSettings(t)
	ctx := context.Background()
	_ = st.Upsert(ctx, &models.NotifySettings{Enabled: true, Kind: "ntfy"})
	st.GetErr = repositories.ErrNotifySettingsUnreadable
	v, err := s.View(ctx)
	if err != nil || !v.Unreadable || v.Enabled {
		t.Fatalf("view %+v %v", v, err)
	}
	var fe *notify.FieldError
	if _, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "ntfy"}, ""); !errors.As(err, &fe) || fe.Field != "url" {
		t.Errorf("save without URL: %v", err)
	}
	if _, err := s.Save(ctx, notify.SettingsInput{Enabled: true, Kind: "ntfy", URL: rcv.URL + "/t"}, ""); err != nil {
		t.Errorf("save with URL: %v", err)
	}
}

func TestSettingsTestButton(t *testing.T) {
	s, rcv, st := newSettings(t)
	ctx := context.Background()
	// Unsaved form values are tested without storing them.
	msg, err := s.Test(ctx, notify.SettingsInput{Kind: "webhook", URL: rcv.URL + topicPath, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Test notification sent to "+rcv.URL || strings.Contains(msg, topicPath) {
		t.Errorf("msg %q", msg)
	}
	if st.Row() != nil {
		t.Error("Test stored settings")
	}
	reqs := rcv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("got %d POSTs", len(reqs))
	}
	if p := reqs[0].Payload(t); p.Event != notify.EventTest || p.Link != "https://forge.example.test/settings/profile" {
		t.Errorf("payload %+v", p)
	}
	// Blank fields fall back to the stored target.
	_, _ = s.Save(ctx, notify.SettingsInput{Kind: "ntfy", URL: rcv.URL + "/stored"}, "")
	if _, err = s.Test(ctx, notify.SettingsInput{Kind: "ntfy"}); err != nil {
		t.Fatal(err)
	}
	if reqs = rcv.Requests(); len(reqs) != 2 || reqs[1].Path != "/stored" || reqs[1].Header.Get("Priority") != "default" {
		t.Errorf("stored-target test: %+v", reqs)
	}
	// A failing receiver gives a SendError naming only the host.
	rcv.Status = http.StatusForbidden
	_, err = s.Test(ctx, notify.SettingsInput{Kind: "ntfy", URL: rcv.URL + topicPath})
	var se *notify.SendError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "403") || strings.Contains(se.Message, topicPath) {
		t.Errorf("send error = %v", err)
	}
	// No URL anywhere: field error, nothing sent.
	s2, rcv2, _ := newSettings(t)
	var fe *notify.FieldError
	if _, err := s2.Test(ctx, notify.SettingsInput{Kind: "ntfy"}); !errors.As(err, &fe) {
		t.Errorf("no url: %v", err)
	}
	if len(rcv2.Requests()) != 0 {
		t.Error("sent without a URL")
	}
}

func TestSendErrorUnreachableNamesHostOnly(t *testing.T) {
	s, _, _ := newSettings(t)
	s.Client = &http.Client{Timeout: time.Second}
	_, err := s.Test(context.Background(), notify.SettingsInput{Kind: "webhook", URL: "http://127.0.0.1:1" + topicPath})
	var se *notify.SendError
	if !errors.As(err, &se) || strings.Contains(se.Message, topicPath) || !strings.Contains(se.Message, "127.0.0.1:1") {
		t.Errorf("err = %v", err)
	}
}
