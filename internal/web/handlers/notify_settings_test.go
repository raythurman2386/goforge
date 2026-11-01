package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/notify/notifytest"
)

// Throwaway values; the receiver is an httptest server.
const (
	notifyTopic = "/alerts-topic-SECRET-never-in-html"
	notifyToken = "tk-SECRET-never-in-html"
	notifyPath  = "/settings/notifications"
)

func notifyHandler(t *testing.T) (*SettingsHandler, *notifytest.Receiver, *notifytest.Store, *fakeAudit) {
	t.Helper()
	rcv := notifytest.NewReceiver(t)
	st := &notifytest.Store{}
	a := &fakeAudit{}
	svc := &notify.SettingsService{Store: st, BaseURL: "https://forge.example.test", Redactor: notify.NewRedactor(), Client: rcv.Client()}
	h := (&SettingsHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).WithAudit(a).WithNotifySettings(svc)
	return h, rcv, st, a
}

func TestNotifySettings_NonAdminForbidden(t *testing.T) {
	h, rcv, st, a := notifyHandler(t)
	form := url.Values{"enabled": {"true"}, "kind": {"ntfy"}, "url": {rcv.URL + notifyTopic}}
	for _, tc := range []struct {
		name string
		user *models.User
		want int
	}{{"anonymous", nil, http.StatusUnauthorized}, {"non-admin", plainUser, http.StatusForbidden}} {
		for _, call := range []struct {
			method, path string
			fn           http.HandlerFunc
		}{{http.MethodPut, notifyPath, h.UpdateNotifySettings}, {http.MethodPost, notifyPath + "/test", h.TestNotifySettings}} {
			rr := httptest.NewRecorder()
			call.fn(rr, backupReq(call.method, call.path, form, tc.user))
			if rr.Code != tc.want {
				t.Errorf("%s %s = %d, want %d", tc.name, call.path, rr.Code, tc.want)
			}
		}
	}
	if st.Row() != nil || len(rcv.Requests()) != 0 || len(a.actions) != 0 {
		t.Fatal("a non-admin request saved, sent, or audited")
	}
}

func TestUpdateNotifySettings_SavesAuditsNamesAndHidesSecrets(t *testing.T) {
	h, rcv, st, a := notifyHandler(t)
	rr := httptest.NewRecorder()
	h.UpdateNotifySettings(rr, backupReq(http.MethodPut, notifyPath, url.Values{
		"enabled": {"true"}, "kind": {"webhook"}, "url": {rcv.URL + notifyTopic}, "token": {notifyToken},
	}, adminUser))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing Cache-Control: no-store")
	}
	body := rr.Body.String()
	for _, want := range []string{`id="notify-settings"`, "Notification settings saved", "Notifications are on", rcv.URL} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q", want)
		}
	}
	if strings.Contains(body, notifyTopic) || strings.Contains(body, notifyToken) {
		t.Fatal("response leaks the URL path or token")
	}
	if r := st.Row(); r == nil || r.URL != rcv.URL+notifyTopic || r.Token != notifyToken || r.UpdatedBy.String != adminUser.ID {
		t.Fatalf("stored %+v", r)
	}
	if len(a.actions) != 1 || a.actions[0] != models.AuditActionNotifySettingsUpdate {
		t.Fatalf("audit %v", a.actions)
	}
	changed, _ := a.details[0]["changed"].([]string)
	if strings.Join(changed, ",") != "enabled,kind,url,token" {
		t.Errorf("audit changed = %v", a.details[0])
	}
	for _, v := range a.details[0] {
		if s, ok := v.(string); ok && (strings.Contains(s, notifyTopic) || strings.Contains(s, notifyToken)) {
			t.Fatal("audit details carry a secret")
		}
	}
	// Saving again with blank secrets keeps them and writes no audit row.
	rr = httptest.NewRecorder()
	h.UpdateNotifySettings(rr, backupReq(http.MethodPut, notifyPath, url.Values{"enabled": {"true"}, "kind": {"webhook"}}, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "No changes to save") || len(a.actions) != 1 {
		t.Fatalf("resave: %d %v", rr.Code, a.actions)
	}
	if st.Row().Token != notifyToken {
		t.Fatal("blank token cleared the stored one")
	}
	if len(rcv.Requests()) != 0 {
		t.Fatal("save sent a notification")
	}
}

func TestUpdateNotifySettings_BadFieldIs400(t *testing.T) {
	h, _, st, a := notifyHandler(t)
	rr := httptest.NewRecorder()
	h.UpdateNotifySettings(rr, backupReq(http.MethodPut, notifyPath, url.Values{"enabled": {"true"}, "kind": {"ntfy"}, "url": {"https://me:pw@ntfy.example.test/t"}}, adminUser))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "URL must not contain a user or password") {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "pw@") {
		t.Fatal("error echoes the URL")
	}
	if st.Row() != nil || len(a.actions) != 0 {
		t.Fatal("bad input was stored or audited")
	}
}

func TestTestNotifySettings_SendsOneAndReports(t *testing.T) {
	h, rcv, st, a := notifyHandler(t)
	rr := httptest.NewRecorder()
	h.TestNotifySettings(rr, backupReq(http.MethodPost, notifyPath+"/test", url.Values{"kind": {"ntfy"}, "url": {rcv.URL + notifyTopic}, "token": {notifyToken}}, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Test notification sent to "+rcv.URL) {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), notifyTopic) {
		t.Fatal("toast leaks the topic")
	}
	reqs := rcv.Requests()
	if len(reqs) != 1 || reqs[0].Path != notifyTopic || reqs[0].Header.Get("Authorization") != "Bearer "+notifyToken {
		t.Fatalf("requests %+v", reqs)
	}
	if st.Row() != nil || len(a.actions) != 0 {
		t.Fatal("Test saved or audited")
	}

	rcv.Status = http.StatusUnauthorized
	rr = httptest.NewRecorder()
	h.TestNotifySettings(rr, backupReq(http.MethodPost, notifyPath+"/test", url.Values{"kind": {"ntfy"}, "url": {rcv.URL + notifyTopic}}, adminUser))
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "answered 401") || strings.Contains(rr.Body.String(), notifyTopic) {
		t.Fatalf("failing receiver: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.TestNotifySettings(rr, backupReq(http.MethodPost, notifyPath+"/test", url.Values{"kind": {"ntfy"}}, adminUser))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("no URL: %d", rr.Code)
	}
}

func TestNotifyForm_AdminOnly(t *testing.T) {
	h, _, _, _ := notifyHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/settings/profile", nil)
	if h.notifyForm(req, adminUser) == nil {
		t.Error("admin gets no form")
	}
	if h.notifyForm(req, plainUser) != nil {
		t.Error("non-admin gets the form")
	}
	if (&SettingsHandler{}).notifyForm(req, adminUser) != nil {
		t.Error("form shown without notification settings wired")
	}
}

func TestNotifySettings_EmailSaveAndTestHideThePassword(t *testing.T) {
	const pass = "smtp-SECRET-never-in-html" //nolint:gosec // throwaway test value
	srv := notifytest.NewSMTPServer(t, models.SMTPTLSStartTLS, "alerts@example.test", pass)
	h, _, st, a := notifyHandler(t)
	h.notifySettings.(*notify.SettingsService).SMTPTLS = srv.ClientTLS
	form := url.Values{
		"enabled": {"true"}, "kind": {"email"}, "smtp_host": {srv.Host}, "smtp_port": {strconv.Itoa(srv.Port)},
		"smtp_tls": {"starttls"}, "smtp_username": {"alerts@example.test"}, "smtp_password": {pass},
		"smtp_from": {"goforge@example.test"}, "smtp_to": {"ops@example.test"},
	}
	rr := httptest.NewRecorder()
	h.UpdateNotifySettings(rr, backupReq(http.MethodPut, notifyPath, form, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Notification settings saved") {
		t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, pass) || !strings.Contains(body, `value="email" selected`) || !strings.Contains(body, "Remove the stored SMTP password") {
		t.Fatalf("card after email save: leak=%v", strings.Contains(body, pass))
	}
	if r := st.Row(); r == nil || r.SMTP.Password != pass || r.Kind != models.NotifyKindEmail {
		t.Fatal("email settings not stored")
	}
	for _, v := range a.details[0] {
		if s, ok := v.(string); ok && strings.Contains(s, pass) {
			t.Fatal("audit details carry the password")
		}
	}

	// Test with a blank password uses the stored one.
	form.Del("smtp_password")
	rr = httptest.NewRecorder()
	h.TestNotifySettings(rr, backupReq(http.MethodPost, notifyPath+"/test", form, adminUser))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Test email sent via "+srv.Addr()) || len(srv.Messages()) != 1 {
		t.Fatalf("test: %d %s", rr.Code, rr.Body.String())
	}

	// A wrong password is a 502 that names neither it nor the server's reply.
	form.Set("smtp_password", "wrong-SECRET-never-in-html")
	rr = httptest.NewRecorder()
	h.TestNotifySettings(rr, backupReq(http.MethodPost, notifyPath+"/test", form, adminUser))
	if rr.Code != http.StatusBadGateway || !strings.Contains(rr.Body.String(), "rejected the username or password") ||
		strings.Contains(rr.Body.String(), "wrong-SECRET") || strings.Contains(rr.Body.String(), "got password") {
		t.Fatalf("bad auth: %d %s", rr.Code, rr.Body.String())
	}

	// A field error names the field.
	form.Set("smtp_to", "not-an-address")
	rr = httptest.NewRecorder()
	h.UpdateNotifySettings(rr, backupReq(http.MethodPut, notifyPath, form, adminUser))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "To must be email addresses") {
		t.Fatalf("bad To: %d %s", rr.Code, rr.Body.String())
	}
}
