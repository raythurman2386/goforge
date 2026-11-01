package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/raythurman2386/goforge/internal/models"
)

// Target is where alerts go: the decrypted notify_settings row.
type Target struct {
	Kind  string
	URL   string
	Token string
	SMTP  models.SMTPSettings
}

// Payload is the JSON body a generic webhook receives. It has no field for
// the notifier URL or token.
type Payload struct {
	Source   string   `json:"source"`
	Event    string   `json:"event"`
	Title    string   `json:"title"`
	Resource Resource `json:"resource"`
	Error    string   `json:"error,omitempty"`
	Link     string   `json:"link,omitempty"`
	AuditID  string   `json:"audit_id,omitempty"`
	Time     string   `json:"time"`
}

// SendError is a failed delivery. Its message is safe to show an admin: it
// never contains the URL or the token.
type SendError struct{ Message string }

func (e *SendError) Error() string { return e.Message }

// buildPayload redacts the event and joins its path to baseURL.
func buildPayload(ev Event, baseURL string, red *Redactor) Payload {
	t := ev.Time
	if t.IsZero() {
		t = time.Now()
	}
	link := ""
	if ev.Path != "" {
		link = strings.TrimRight(baseURL, "/") + ev.Path
	}
	return Payload{
		Source:   "goforge",
		Event:    ev.Kind,
		Title:    ev.title(),
		Resource: ev.Resource,
		Error:    red.Redact(ev.Error),
		Link:     link,
		AuditID:  ev.AuditID,
		Time:     t.UTC().Format(time.RFC3339),
	}
}

// headerSafe drops control and non-ASCII characters from a header value.
func headerSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ntfyTags maps an event to ntfy emoji tags.
func ntfyTags(kind string) string {
	switch kind {
	case EventTest:
		return "white_check_mark,goforge"
	case EventContainerUnhealthy:
		return "warning,goforge"
	default:
		return "rotating_light,goforge"
	}
}

// newRequest builds the HTTP request for one payload.
func newRequest(ctx context.Context, tgt Target, p Payload) (*http.Request, error) {
	var req *http.Request
	var err error
	switch tgt.Kind {
	case models.NotifyKindNtfy:
		body := p.Resource.Type + " " + p.Resource.ID
		if p.Error != "" {
			body = p.Error + "\n" + body
		}
		if p.AuditID != "" {
			body += "\naudit_id " + p.AuditID
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, tgt.URL, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		req.Header.Set("Title", headerSafe(p.Title))
		req.Header.Set("Tags", ntfyTags(p.Event))
		if p.Event == EventTest {
			req.Header.Set("Priority", "default")
		} else {
			req.Header.Set("Priority", "high")
		}
		if p.Link != "" {
			req.Header.Set("Click", headerSafe(p.Link))
		}
	case models.NotifyKindWebhook:
		b, mErr := json.Marshal(p)
		if mErr != nil {
			return nil, mErr
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, tgt.URL, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	default:
		return nil, fmt.Errorf("unknown notifier kind %q", tgt.Kind)
	}
	req.Header.Set("User-Agent", "GoForge-Notifier/1")
	if tgt.Token != "" {
		req.Header.Set("Authorization", "Bearer "+tgt.Token)
	}
	return req, nil
}

// deliver sends one payload: an HTTP POST for ntfy and webhook, SMTP for
// email (tlsCfg, when set, is the base TLS config; tests use it to trust a
// fake server). Errors are *SendError and never name the URL path, the
// token, or SMTP credentials.
func deliver(ctx context.Context, client *http.Client, tlsCfg *tls.Config, tgt Target, p Payload) error {
	if tgt.Kind == models.NotifyKindEmail {
		return deliverEmail(ctx, tlsCfg, tgt.SMTP, p)
	}
	req, err := newRequest(ctx, tgt, p)
	if err != nil {
		return &SendError{Message: "could not build the request"}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &SendError{Message: "timed out"}
		}
		return &SendError{Message: "could not reach " + hostOf(tgt.URL)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &SendError{Message: fmt.Sprintf("%s answered %d", hostOf(tgt.URL), resp.StatusCode)}
	}
	return nil
}
