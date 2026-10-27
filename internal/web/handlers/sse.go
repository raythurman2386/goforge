package handlers

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/deploy"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/project"
	"github.com/raythurman2386/goforge/internal/sse"
	"github.com/raythurman2386/goforge/internal/web/templates/components"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/deployments"
)

// SSEHandler handles Server-Sent Events for log streaming.
type SSEHandler struct {
	hub            *sse.Hub
	dockerClient   *docker.Client
	collector      *monitor.Collector
	containerRepo  *repositories.ContainerRepository
	deployService  *deploy.Service
	projectService *project.Service
	logger         *slog.Logger
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(hub *sse.Hub, dc *docker.Client, c *monitor.Collector,
	cr *repositories.ContainerRepository, _ *repositories.ServiceInstanceRepository,
	ds *deploy.Service, ps *project.Service, logger *slog.Logger,
) *SSEHandler {
	return &SSEHandler{
		hub:            hub,
		dockerClient:   dc,
		collector:      c,
		containerRepo:  cr,
		deployService:  ds,
		projectService: ps,
		logger:         logger,
	}
}

// setSSEHeaders sets the standard headers for SSE responses.
func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

// DeploymentLogs streams deployment logs via SSE.
// GET /deployments/{id}/logs
func (h *SSEHandler) DeploymentLogs(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "deploymentID")
	if deploymentID == "" {
		http.Error(w, "deployment ID is required", http.StatusBadRequest)
		return
	}

	// Verify deployment ownership via deployment -> environment -> project -> user
	if !h.verifyDeploymentOwnership(w, r, deploymentID) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setSSEHeaders(w)

	topic := fmt.Sprintf("deployment:%s", deploymentID)
	ch := make(chan sse.Event, 256)
	h.hub.RegisterClient(ch, topic)
	defer h.hub.UnregisterClient(ch)

	writeEvent(w, flusher, "connected", map[string]string{
		"topic": topic,
		"id":    deploymentID,
	})

	seen := map[string]struct{}{}
	if h.deployService != nil {
		logs, logErr := h.deployService.GetLogs(r.Context(), deploymentID)
		if logErr != nil {
			h.logger.Warn("failed to load deployment logs", "deployment_id", deploymentID, "error", logErr)
		}
		for _, entry := range logs {
			if entry.ID != "" {
				seen[entry.ID] = struct{}{}
			}
		}
		writeDeploymentSnapshot(w, flusher, r, logs)
	}

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if status, isStatus := deploymentStatusFromEvent(event.Data); isStatus {
				writeDeploymentStatus(w, flusher, r, status)
				continue
			}
			entry, isLog := deploymentLogFromEvent(event.Data)
			if !isLog {
				continue
			}
			if _, dup := seen[entry.ID]; dup && entry.ID != "" {
				continue
			}
			if entry.ID != "" {
				seen[entry.ID] = struct{}{}
			}
			writeDeploymentLog(w, flusher, r, entry)
		case <-r.Context().Done():
			return
		}
	}
}

// ContainerLogs streams container logs via SSE.
// GET /containers/{id}/logs
func (h *SSEHandler) ContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		http.Error(w, "container ID is required", http.StatusBadRequest)
		return
	}

	// Verify container ownership
	if !h.verifyContainerOwnership(w, r, containerID) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	if h.dockerClient == nil {
		http.Error(w, "docker unavailable", http.StatusServiceUnavailable)
		return
	}

	setSSEHeaders(w)
	writeEvent(w, flusher, "connected", map[string]string{
		"id": containerID,
	})

	reader, err := h.dockerClient.StreamLogs(r.Context(), containerID, true)
	if err != nil {
		h.logger.Error("failed to stream container logs", "container_id", containerID, "error", err)
		writeContainerLogLine(w, flusher, "failed to read container logs")
		return
	}
	defer reader.Close()

	pr, pw := io.Pipe()
	go func() {
		_, copyErr := stdcopy.StdCopy(pw, pw, reader)
		_ = pw.CloseWithError(copyErr)
	}()

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-r.Context().Done():
			return
		default:
			writeContainerLogLine(w, flusher, scanner.Text())
		}
	}
}

// ContainerStats streams container stats via SSE.
// GET /containers/{id}/stats
func (h *SSEHandler) ContainerStats(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		http.Error(w, "container ID is required", http.StatusBadRequest)
		return
	}

	// Verify container ownership
	if !h.verifyContainerOwnership(w, r, containerID) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setSSEHeaders(w)

	// Send initial connection event
	writeEvent(w, flusher, "connected", map[string]string{
		"id": containerID,
	})

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats, err := h.collector.CollectStats(r.Context(), containerID)
			if err != nil {
				h.logger.Debug("failed to collect container stats", "container_id", containerID, "error", err)
				continue
			}

			var buf bytes.Buffer
			if err := components.Stats(stats).Render(r.Context(), &buf); err != nil {
				h.logger.Debug("failed to render stats component", "container_id", containerID, "error", err)
				continue
			}
			writeRawEvent(w, flusher, "stats", buf.Bytes())

		case <-r.Context().Done():
			return
		}
	}
}

// BuildOutput streams build output via SSE.
// GET /builds/{id}/output
func (h *SSEHandler) BuildOutput(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, "build ID is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setSSEHeaders(w)

	topic := fmt.Sprintf("build:%s", buildID)
	ch := make(chan sse.Event, 10)
	h.hub.RegisterClient(ch, topic)
	defer h.hub.UnregisterClient(ch)

	writeEvent(w, flusher, "connected", map[string]string{
		"topic": topic,
		"id":    buildID,
	})

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeEvent(w, flusher, event.Topic, event.Data)
		case <-r.Context().Done():
			return
		}
	}
}

// writeEvent writes a formatted SSE event to the response.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		jsonData = []byte(`{"error": "marshal failed"}`)
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
	flusher.Flush()
}

// writeRawEvent writes raw data to the response.
func writeRawEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprintf(w, "\n")
	flusher.Flush()
}

// verifyContainerOwnership checks that the Docker container belongs to the
// authenticated user via a service instance or deployment → environment → project.
func (h *SSEHandler) verifyContainerOwnership(w http.ResponseWriter, r *http.Request, dockerID string) bool {
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	if h.containerRepo == nil {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}

	owned, err := h.containerRepo.OwnedByUser(r.Context(), dockerID, user.ID)
	if err != nil || !owned {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}
	return true
}

func writeDeploymentLog(w http.ResponseWriter, flusher http.Flusher, r *http.Request, entry *models.DeploymentLog) {
	var buf bytes.Buffer
	if err := deployments.LogLine(entry).Render(r.Context(), &buf); err != nil {
		return
	}
	writeRawEvent(w, flusher, "log", buf.Bytes())
}

func writeDeploymentSnapshot(w http.ResponseWriter, flusher http.Flusher, r *http.Request, logs []*models.DeploymentLog) {
	var buf bytes.Buffer
	if len(logs) == 0 {
		buf.WriteString(`<p class="text-gray-500 italic">No logs available yet...</p>`)
	}
	for _, entry := range logs {
		if err := deployments.LogLine(entry).Render(r.Context(), &buf); err != nil {
			return
		}
	}
	writeRawEvent(w, flusher, "snapshot", buf.Bytes())
}

func writeDeploymentStatus(w http.ResponseWriter, flusher http.Flusher, r *http.Request, status models.DeploymentStatus) {
	var buf bytes.Buffer
	if err := components.DeploymentStatusBadge(status).Render(r.Context(), &buf); err != nil {
		return
	}
	writeRawEvent(w, flusher, "status", buf.Bytes())
}

func deploymentStatusFromEvent(data interface{}) (models.DeploymentStatus, bool) {
	payload, ok := data.(map[string]interface{})
	if !ok {
		return "", false
	}
	kind, kindOK := payload["kind"].(string)
	if !kindOK || kind != "status" {
		return "", false
	}
	switch status := payload["status"].(type) {
	case models.DeploymentStatus:
		return status, status != ""
	case string:
		parsed := models.DeploymentStatus(status)
		return parsed, parsed != ""
	default:
		return "", false
	}
}

func deploymentLogFromEvent(data interface{}) (*models.DeploymentLog, bool) {
	payload, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}
	kind, kindOK := payload["kind"].(string)
	if !kindOK || kind != "log" {
		return nil, false
	}
	message, messageOK := payload["message"].(string)
	if !messageOK || message == "" {
		return nil, false
	}
	level, levelOK := payload["level"].(string)
	if !levelOK {
		level = "info"
	}
	source, sourceOK := payload["source"].(string)
	if !sourceOK {
		source = ""
	}
	id, idOK := payload["id"].(string)
	if !idOK {
		id = ""
	}
	entry := &models.DeploymentLog{
		ID:        id,
		Level:     models.DeploymentLogLevel(level),
		Message:   message,
		Source:    sql.NullString{String: source, Valid: source != ""},
		Timestamp: time.Now(),
	}
	if ts, ok := payload["timestamp"].(time.Time); ok && !ts.IsZero() {
		entry.Timestamp = ts
	}
	return entry, true
}

func writeContainerLogLine(w http.ResponseWriter, flusher http.Flusher, line string) {
	var buf bytes.Buffer
	buf.WriteString(`<div class="py-0.5">`)
	buf.WriteString(html.EscapeString(line))
	buf.WriteString(`</div>`)
	writeRawEvent(w, flusher, "log", buf.Bytes())
}

// verifyDeploymentOwnership checks that a deployment belongs to the authenticated
// user by tracing deployment → environment → project → user.
// Returns true if ownership is verified; writes an HTTP error and returns false otherwise.
func (h *SSEHandler) verifyDeploymentOwnership(w http.ResponseWriter, r *http.Request, deploymentID string) bool {
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	deployment, err := h.deployService.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return false
	}

	env, err := h.projectService.GetEnvironment(r.Context(), deployment.EnvironmentID)
	if err != nil {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}

	// The deployment id in the path must belong to the project id in the path.
	if pathProjectID := chi.URLParam(r, "id"); pathProjectID != "" && env.ProjectID != pathProjectID {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return false
	}

	proj, err := h.projectService.GetProject(r.Context(), env.ProjectID)
	if err != nil || proj.UserID != user.ID {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}

	return true
}
