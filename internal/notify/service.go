package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
)

// sendTimeout bounds one delivery, including Test.
const sendTimeout = 10 * time.Second

// Store is the notify_settings row.
type Store interface {
	// Get returns repositories.ErrNotFound when no row exists.
	Get(ctx context.Context) (*models.NotifySettings, error)
	Upsert(ctx context.Context, s *models.NotifySettings) error
}

// Service is the outbound notifier. Notify reads the saved settings each
// time, so a save applies without a restart, and sends in the background.
type Service struct {
	Store Store
	// BaseURL is GOFORGE_BASE_URL; links are BaseURL + Event.Path.
	BaseURL  string
	Redactor *Redactor
	Client   *http.Client
	// SMTPTLS is the base TLS config for email; nil uses the system roots.
	SMTPTLS *tls.Config
	Logger  *slog.Logger

	wg sync.WaitGroup
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: sendTimeout}
}

// target returns the saved target, or ok=false when notifications are off,
// not configured, or unreadable.
func (s *Service) target(ctx context.Context) (Target, bool) {
	if s == nil || s.Store == nil {
		return Target{}, false
	}
	row, err := s.Store.Get(ctx)
	switch {
	case errors.Is(err, repositories.ErrNotFound):
		return Target{}, false
	case err != nil:
		s.logger().Warn("notifications skipped: settings unreadable", "error", err)
		return Target{}, false
	}
	if !row.Enabled || !configured(*row) {
		return Target{}, false
	}
	return Target{Kind: row.Kind, URL: row.URL, Token: row.Token, SMTP: row.SMTP}, true
}

// configured reports whether the row has what its kind needs to send.
func configured(row models.NotifySettings) bool {
	if row.Kind == models.NotifyKindEmail {
		return row.SMTP.Host != "" && row.SMTP.From != "" && row.SMTP.To != ""
	}
	return row.URL != ""
}

// Enabled reports whether a saved, enabled notifier exists.
func (s *Service) Enabled(ctx context.Context) bool {
	_, ok := s.target(ctx)
	return ok
}

// Notify sends ev in the background when notifications are on. It returns
// at once. Delivery errors are logged with the event kind and resource,
// never the URL or the payload.
func (s *Service) Notify(ctx context.Context, ev Event) {
	if s == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
		defer cancel()
		tgt, ok := s.target(sendCtx)
		if !ok {
			return
		}
		p := buildPayload(ev, s.BaseURL, s.Redactor)
		log := s.logger().With("event", ev.Kind, "resource_type", ev.Resource.Type, "resource_id", ev.Resource.ID)
		if err := deliver(sendCtx, s.client(), s.SMTPTLS, tgt, p); err != nil {
			log.Warn("notification not delivered", "error", err.Error())
			return
		}
		log.Info("notification sent", "kind", tgt.Kind)
	}()
}

// Wait blocks until every Notify started so far has finished. Serve calls
// it on shutdown and `goforge backup run` before it exits.
func (s *Service) Wait() {
	if s != nil {
		s.wg.Wait()
	}
}

// hostOf is the scheme and host of a URL, for messages. The path (an ntfy
// topic or webhook secret) is never shown.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "the notifier URL"
	}
	return u.Scheme + "://" + u.Host
}
