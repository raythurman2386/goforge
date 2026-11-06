package handlers

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"

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

// Stats stream timing. The first frame needs two samples for a CPU delta, so
// the second sample follows the first after statsPrimeDelay; later frames
// come every statsInterval.
const (
	statsPrimeDelay = 500 * time.Millisecond
	statsInterval   = 2 * time.Second
)

// statsSampler takes one raw one-shot stats sample; *monitor.Collector
// satisfies it.
type statsSampler interface {
	SampleStats(ctx context.Context, dockerID string) (*container.StatsResponse, error)
}

// SSEHandler handles Server-Sent Events for log streaming.
type SSEHandler struct {
	hub            *sse.Hub
	dockerClient   *docker.Client
	stats          statsSampler
	statsPrime     time.Duration
	statsInterval  time.Duration
	ownsContainer  func(ctx context.Context, dockerID, userID string) (bool, error)
	deployService  *deploy.Service
	projectService *project.Service
	logger         *slog.Logger
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(hub *sse.Hub, dc *docker.Client, c *monitor.Collector,
	cr *repositories.ContainerRepository, _ *repositories.ServiceInstanceRepository,
	ds *deploy.Service, ps *project.Service, logger *slog.Logger,
) *SSEHandler {
	h := &SSEHandler{
		hub:            hub,
		dockerClient:   dc,
		statsPrime:     statsPrimeDelay,
		statsInterval:  statsInterval,
		deployService:  ds,
		projectService: ps,
		logger:         logger,
	}
	// Leave the interfaces nil, not typed-nil, when a dependency is missing.
	if c != nil {
		h.stats = c
	}
	if cr != nil {
		h.ownsContainer = cr.OwnedByUser
	}
	return h
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

	seen := h.writeDeploymentSnapshot(w, flusher, r, deploymentID)

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if status, isStatus := deploymentStatusFromEvent(event.Data); isStatus {
				h.writeDeploymentStatusUpdate(w, flusher, r, deploymentID, status)
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

	h.streamStats(w, flusher, r, containerID)
}

// streamStats sends a stats frame from each pair of one-shot samples, the
// first about statsPrime after connect. When the container is gone or not
// running it sends one unavailable frame per distinct reason and keeps
// sampling, so the card recovers if the container comes back.
func (h *SSEHandler) streamStats(w http.ResponseWriter, flusher http.Flusher, r *http.Request, containerID string) {
	ctx := r.Context()
	var prev *container.StatsResponse
	lastReason := ""
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		cur, reason := h.sampleStats(ctx, containerID)
		if ctx.Err() != nil {
			return
		}
		switch {
		case reason != "":
			prev = nil
			if reason != lastReason {
				writeStatsFragment(w, flusher, r, components.StatsUnavailable(reason))
				lastReason = reason
			}
			timer.Reset(h.statsInterval)
		case prev == nil:
			// Keep the first good sample and take the next one soon, so the
			// first frame carries a real CPU delta.
			prev = cur
			timer.Reset(h.statsPrime)
		default:
			writeStatsFragment(w, flusher, r, components.Stats(monitor.MetricsFromSamples(containerID, prev, cur)))
			prev = cur
			lastReason = ""
			timer.Reset(h.statsInterval)
		}
	}
}

// sampleStats returns a sample, or a user-facing reason when there is none.
func (h *SSEHandler) sampleStats(ctx context.Context, containerID string) (sample *container.StatsResponse, reason string) {
	if h.stats == nil {
		return nil, "Metrics are not available: Docker is not connected."
	}
	cur, err := h.stats.SampleStats(ctx, containerID)
	if err != nil {
		h.logger.Debug("failed to collect container stats", "container_id", containerID, "error", err)
		return nil, "Metrics are not available: the container was not found or Docker did not answer."
	}
	if monitor.SampleNotRunning(cur) {
		return nil, "Metrics are not available: the container is not running."
	}
	return cur, ""
}

// writeStatsFragment sends one stats or unavailable frame as an unnamed
// message; the card's hx-target swaps it into the [data-metrics-frame] child.
func writeStatsFragment(w http.ResponseWriter, flusher http.Flusher, r *http.Request, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		return
	}
	writeMessage(w, flusher, buf.Bytes())
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

// writeMessage writes an unnamed SSE message (no event: line). htmx 4
// hx-sse swaps it into the connect element's target (hx-target, hx-swap);
// a named event would only be dispatched as a DOM event on the element.
func writeMessage(w http.ResponseWriter, flusher http.Flusher, data []byte) {
	for _, line := range strings.Split(string(data), "\n") {
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
	if h.ownsContainer == nil {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}

	owned, err := h.ownsContainer(r.Context(), dockerID, user.ID)
	if err != nil || !owned {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}
	return true
}

// The deployment log stream's connect element swaps nothing itself
// (hx-swap="none"); each unnamed message carries one out-of-band swap.

// writeDeploymentLog appends one line to the log list.
func writeDeploymentLog(w http.ResponseWriter, flusher http.Flusher, r *http.Request, entry *models.DeploymentLog) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<div hx-swap-oob="beforeend:#%s">`, deployments.LogLinesID)
	if err := deployments.LogLine(entry).Render(r.Context(), &buf); err != nil {
		return
	}
	buf.WriteString(`</div>`)
	writeMessage(w, flusher, buf.Bytes())
}

// writeDeploymentSnapshot replaces the log list with the stored lines, so a
// reconnect does not duplicate the lines the page already shows.
func writeDeploymentSnapshot(w http.ResponseWriter, flusher http.Flusher, r *http.Request, logs []*models.DeploymentLog) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<div id="%s" hx-swap-oob="innerHTML">`, deployments.LogLinesID)
	if len(logs) == 0 {
		buf.WriteString(`<p class="text-foreground-secondary italic">No logs available yet...</p>`)
	}
	for _, entry := range logs {
		if err := deployments.LogLine(entry).Render(r.Context(), &buf); err != nil {
			return
		}
	}
	buf.WriteString(`</div>`)
	writeMessage(w, flusher, buf.Bytes())
}

// writeDeploymentSnapshot replays stored logs once and returns their IDs so
// live events racing the snapshot are not appended twice.
func (h *SSEHandler) writeDeploymentSnapshot(w http.ResponseWriter, flusher http.Flusher, r *http.Request, deploymentID string) map[string]struct{} {
	seen := make(map[string]struct{})
	if h.deployService == nil {
		return seen
	}
	logs, err := h.deployService.GetLogs(r.Context(), deploymentID)
	if err != nil {
		h.logger.Warn("failed to load deployment logs", "deployment_id", deploymentID, "error", err)
	}
	for _, entry := range logs {
		if entry.ID != "" {
			seen[entry.ID] = struct{}{}
		}
	}
	writeDeploymentSnapshot(w, flusher, r, logs)
	h.writeCurrentDeploymentProgress(w, flusher, r, deploymentID)
	return seen
}

func (h *SSEHandler) writeDeploymentStatusUpdate(w http.ResponseWriter, flusher http.Flusher, r *http.Request, deploymentID string, status models.DeploymentStatus) {
	writeDeploymentStatus(w, flusher, r, status)
	h.writeCurrentDeploymentProgress(w, flusher, r, deploymentID)
}

// writeCurrentDeploymentProgress reads the persisted stage and timestamps so
// both an initial stream and a status event update the same summary.
func (h *SSEHandler) writeCurrentDeploymentProgress(w http.ResponseWriter, flusher http.Flusher, r *http.Request, deploymentID string) {
	deployment, err := h.deployService.GetDeployment(r.Context(), deploymentID)
	if err != nil {
		return
	}
	writeDeploymentProgress(w, flusher, r, deployment)
}

// writeDeploymentStatus replaces the live status badge.
func writeDeploymentStatus(w http.ResponseWriter, flusher http.Flusher, r *http.Request, status models.DeploymentStatus) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<span id="%s" hx-swap-oob="innerHTML">`, deployments.LiveStatusID)
	if err := components.DeploymentStatusBadge(status).Render(r.Context(), &buf); err != nil {
		return
	}
	buf.WriteString(`</span>`)
	writeMessage(w, flusher, buf.Bytes())
}

// writeDeploymentProgress replaces the stage and elapsed summary with fresh
// persisted timestamps after each status transition and on reconnect.
func writeDeploymentProgress(w http.ResponseWriter, flusher http.Flusher, r *http.Request, deployment *models.Deployment) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<div id="%s" hx-swap-oob="innerHTML">`, components.DeploymentProgressID)
	if err := components.DeploymentProgress(deployment).Render(r.Context(), &buf); err != nil {
		return
	}
	buf.WriteString(`</div>`)
	writeMessage(w, flusher, buf.Bytes())
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
	writeMessage(w, flusher, buf.Bytes())
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
