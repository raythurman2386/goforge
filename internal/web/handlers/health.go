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

// HealthHandler handles health check requests.
type HealthHandler struct {
	db           *database.DB
	dockerClient *docker.Client
	logger       *slog.Logger
}

// NewHealthHandler creates a new HealthHandler.
func NewHealthHandler(db *database.DB, dockerClient *docker.Client, logger *slog.Logger) *HealthHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &HealthHandler{
		db:           db,
		dockerClient: dockerClient,
		logger:       logger,
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
func (h *HealthHandler) HealthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status := "ready"
	statusCode := http.StatusOK
	checks := make(map[string]string)

	// Check database connectivity
	if h.db != nil {
		if err := h.db.HealthCheck(ctx); err != nil {
			checks["database"] = "unavailable: " + err.Error()
			status = "not_ready"
			statusCode = http.StatusServiceUnavailable
		} else {
			checks["database"] = "ok"
		}
	} else {
		checks["database"] = "not_configured"
	}

	// Check Docker connectivity
	if h.dockerClient != nil {
		if err := h.dockerClient.Ping(ctx); err != nil {
			checks["docker"] = "unavailable: " + err.Error()
			status = "not_ready"
			statusCode = http.StatusServiceUnavailable
		} else {
			checks["docker"] = "ok"
		}
	} else {
		checks["docker"] = "not_configured"
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
