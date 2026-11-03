package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/raythurman2386/goforge/internal/database"
	"github.com/raythurman2386/goforge/internal/docker"
)

// Readiness check values. /health/ready is public, so these are the only
// values a caller ever sees in "checks". The underlying error goes to the
// server log, never to the response body (HEALTH-READY-PUBLIC).
const (
	healthCheckOK            = "ok"
	healthCheckUnavailable   = "unavailable"
	healthCheckNotConfigured = "not_configured"
)

// healthChecker is one readiness dependency. *database.DB and *docker.Client
// satisfy it through small adapters so tests can inject failures.
type healthChecker func(ctx context.Context) error

// HealthHandler handles health check requests.
type HealthHandler struct {
	checkDB     healthChecker
	checkDocker healthChecker
	logger      *slog.Logger
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db *database.DB, dockerClient *docker.Client, logger *slog.Logger) *HealthHandler {
	var checkDB, checkDocker healthChecker
	if db != nil {
		checkDB = db.HealthCheck
	}
	if dockerClient != nil {
		checkDocker = dockerClient.Ping
	}
	return newHealthHandler(checkDB, checkDocker, logger)
}

// newHealthHandler builds a HealthHandler from check functions. A nil check
// reports "not_configured".
func newHealthHandler(checkDB, checkDocker healthChecker, logger *slog.Logger) *HealthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &HealthHandler{
		checkDB:     checkDB,
		checkDocker: checkDocker,
		logger:      logger,
	}
}

// Health returns a simple liveness check.
// This endpoint should return quickly and indicate the application is running.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode health response", "error", err)
	}
}

// HealthReady returns a readiness check that verifies dependencies.
// This checks database connectivity and Docker daemon availability.
//
// The route is public. Each check reports only "ok", "unavailable", or
// "not_configured"; the error behind "unavailable" is logged at warn level
// with the check name and is never written to the response.
func (h *HealthHandler) HealthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := map[string]string{
		"database": h.runCheck(ctx, "database", h.checkDB),
		"docker":   h.runCheck(ctx, "docker", h.checkDocker),
	}

	status := "ready"
	statusCode := http.StatusOK
	for _, v := range checks {
		if v == healthCheckUnavailable {
			status = "not_ready"
			statusCode = http.StatusServiceUnavailable
			break
		}
	}

	response := map[string]interface{}{
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"checks":    checks,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("failed to encode ready check response", "error", err)
	}
}

// runCheck runs one readiness check and maps it to a public value.
func (h *HealthHandler) runCheck(ctx context.Context, name string, check healthChecker) string {
	if check == nil {
		return healthCheckNotConfigured
	}
	if err := check(ctx); err != nil {
		h.logger.Warn("readiness check failed", "check", name, "error", err)
		return healthCheckUnavailable
	}
	return healthCheckOK
}
