package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Error text that a real driver or Docker client could return. None of it may
// reach the public /health/ready body.
const (
	leakDBErr     = "failed to connect to `host=db.internal user=goforge database=goforge`: dial tcp 10.0.0.5:5432: connect: connection refused"
	leakDockerErr = "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"
)

func okCheck(context.Context) error { return nil }

func failCheck(msg string) healthChecker {
	return func(context.Context) error { return errors.New(msg) }
}

type readyBody struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Checks    map[string]string `json:"checks"`
}

func runReady(t *testing.T, h *HealthHandler) (*httptest.ResponseRecorder, readyBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.HealthReady(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	var body readyBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return rec, body
}

func TestHealthReady_Values(t *testing.T) {
	tests := []struct {
		name       string
		db, docker healthChecker
		wantCode   int
		wantStatus string
		wantDB     string
		wantDocker string
	}{
		{"all ok", okCheck, okCheck, http.StatusOK, "ready", "ok", "ok"},
		{"nothing configured", nil, nil, http.StatusOK, "ready", "not_configured", "not_configured"},
		{"docker not configured", okCheck, nil, http.StatusOK, "ready", "ok", "not_configured"},
		{"db down", failCheck(leakDBErr), okCheck, http.StatusServiceUnavailable, "not_ready", "unavailable", "ok"},
		{"docker down", okCheck, failCheck(leakDockerErr), http.StatusServiceUnavailable, "not_ready", "ok", "unavailable"},
		{"both down", failCheck(leakDBErr), failCheck(leakDockerErr), http.StatusServiceUnavailable, "not_ready", "unavailable", "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHealthHandler(tt.db, tt.docker, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
			rec, body := runReady(t, h)
			if rec.Code != tt.wantCode {
				t.Errorf("status code = %d, want %d", rec.Code, tt.wantCode)
			}
			if body.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", body.Status, tt.wantStatus)
			}
			if len(body.Checks) != 2 {
				t.Errorf("checks = %v, want exactly database and docker", body.Checks)
			}
			if body.Checks["database"] != tt.wantDB {
				t.Errorf("checks.database = %q, want %q", body.Checks["database"], tt.wantDB)
			}
			if body.Checks["docker"] != tt.wantDocker {
				t.Errorf("checks.docker = %q, want %q", body.Checks["docker"], tt.wantDocker)
			}
			for k, v := range body.Checks {
				switch v {
				case "ok", "unavailable", "not_configured":
				default:
					t.Errorf("checks.%s = %q, not one of ok, unavailable, not_configured", k, v)
				}
			}
		})
	}
}

// HEALTH-READY-PUBLIC: the body must not carry error text, hosts, or socket
// paths, and the real error must be in the server log.
func TestHealthReady_NoErrorTextInBodyAndErrorLogged(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := newHealthHandler(failCheck(leakDBErr), failCheck(leakDockerErr), logger)

	rec, body := runReady(t, h)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want 503", rec.Code)
	}

	raw := rec.Body.String()
	for _, leak := range []string{
		"docker.sock", "unix://", "/var/run", "Cannot connect", "daemon",
		"db.internal", "10.0.0.5", "5432", "connection refused", "dial tcp", "host=", "user=",
	} {
		if strings.Contains(raw, leak) {
			t.Errorf("body leaks %q: %s", leak, raw)
		}
	}
	if body.Checks["database"] != "unavailable" || body.Checks["docker"] != "unavailable" {
		t.Errorf("checks = %v, want both unavailable", body.Checks)
	}

	logs := logBuf.String()
	for _, want := range []struct{ check, err string }{
		{"database", leakDBErr},
		{"docker", leakDockerErr},
	} {
		found := false
		for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				continue
			}
			if entry["msg"] == "readiness check failed" && entry["check"] == want.check && entry["error"] == want.err && entry["level"] == "WARN" {
				found = true
			}
		}
		if !found {
			t.Errorf("no WARN log line for check %q with its error; logs:\n%s", want.check, logs)
		}
	}
}

func TestNewHealthHandler_NilDependenciesNotConfigured(t *testing.T) {
	h := NewHealthHandler(nil, nil, nil)
	rec, body := runReady(t, h)
	if rec.Code != http.StatusOK {
		t.Errorf("status code = %d, want 200", rec.Code)
	}
	if body.Checks["database"] != "not_configured" || body.Checks["docker"] != "not_configured" {
		t.Errorf("checks = %v, want both not_configured", body.Checks)
	}
}
