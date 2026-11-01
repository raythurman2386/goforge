// Package notifytest has test doubles for the notifier: an in-memory
// settings store and an httptest receiver that records every POST. Tests
// point the notifier at the receiver, never at a real ntfy or webhook.
package notifytest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
)

// Store is an in-memory notify.Store.
type Store struct {
	mu  sync.Mutex
	row *models.NotifySettings
	// GetErr, when set, is returned by Get (with the row, if any).
	GetErr error
}

// Get returns the row or repositories.ErrNotFound.
func (s *Store) Get(_ context.Context) (*models.NotifySettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.GetErr != nil {
		if s.row == nil {
			return nil, s.GetErr
		}
		cp := *s.row
		return &cp, s.GetErr
	}
	if s.row == nil {
		return nil, repositories.ErrNotFound
	}
	cp := *s.row
	return &cp, nil
}

// Upsert stores a copy of row.
func (s *Store) Upsert(_ context.Context, row *models.NotifySettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *row
	cp.UpdatedAt = time.Now()
	if s.row == nil {
		cp.CreatedAt = cp.UpdatedAt
	} else {
		cp.CreatedAt = s.row.CreatedAt
	}
	s.row = &cp
	return nil
}

// Row returns a copy of the stored row, or nil.
func (s *Store) Row() *models.NotifySettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.row == nil {
		return nil
	}
	cp := *s.row
	return &cp
}

// Request is one POST the receiver got.
type Request struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

// Payload decodes a webhook body.
func (r Request) Payload(t testing.TB) notify.Payload {
	t.Helper()
	var p notify.Payload
	if err := json.Unmarshal([]byte(r.Body), &p); err != nil {
		t.Fatalf("decode webhook body %q: %v", r.Body, err)
	}
	return p
}

// Raw is the method, path, headers, and body as one string, for "this
// secret appears nowhere" checks.
func (r Request) Raw() string {
	s := r.Method + " " + r.Path + "\n"
	for k, vs := range r.Header {
		for _, v := range vs {
			s += k + ": " + v + "\n"
		}
	}
	return s + "\n" + r.Body
}

// Receiver is an httptest server that records requests.
type Receiver struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []Request
	// Status is the reply code (default 200).
	Status int
}

// NewReceiver starts a receiver closed at test cleanup.
func NewReceiver(t testing.TB) *Receiver {
	t.Helper()
	r := &Receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.reqs = append(r.reqs, Request{Method: req.Method, Path: req.URL.Path, Header: req.Header.Clone(), Body: string(b)})
		status := r.Status
		r.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(r.Close)
	return r
}

// Requests returns a copy of what was received.
func (r *Receiver) Requests() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Request(nil), r.reqs...)
}

// Setup returns an enabled notify.Service whose store points at a new
// receiver with the given kind and path (the secret part of the URL).
func Setup(t testing.TB, kind, path, token string) (*notify.Service, *Receiver, *Store) {
	t.Helper()
	rcv := NewReceiver(t)
	st := &Store{}
	if err := st.Upsert(context.Background(), &models.NotifySettings{Enabled: true, Kind: kind, URL: rcv.URL + path, Token: token}); err != nil {
		t.Fatal(err)
	}
	svc := &notify.Service{Store: st, BaseURL: "https://forge.example.test", Redactor: notify.NewRedactor(), Client: rcv.Client()}
	return svc, rcv, st
}
