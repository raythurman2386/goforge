package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/monitor"
	"github.com/raythurman2386/goforge/internal/sse"
	"github.com/raythurman2386/goforge/internal/web/templates/partials"
)

// SSEHandler handles Server-Sent Events for log streaming.
type SSEHandler struct {
	hub          *sse.Hub
	dockerClient *docker.Client
	collector    *monitor.Collector
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(hub *sse.Hub, dc *docker.Client, c *monitor.Collector) *SSEHandler {
	return &SSEHandler{
		hub:          hub,
		dockerClient: dc,
		collector:    c,
	}
}

// setSSEHeaders sets the standard headers for SSE responses.
func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// DeploymentLogs streams deployment logs via SSE.
// GET /deployments/{id}/logs
func (h *SSEHandler) DeploymentLogs(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "id")
	if deploymentID == "" {
		http.Error(w, "deployment ID is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setSSEHeaders(w)

	topic := fmt.Sprintf("deployment:%s", deploymentID)
	ch := make(chan sse.Event, 10)
	h.hub.RegisterClient(ch, topic)
	defer h.hub.UnregisterClient(ch)

	// Send initial connection event
	writeEvent(w, flusher, "connected", map[string]string{
		"topic": topic,
		"id":    deploymentID,
	})

	// Stream events until context is canceled
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

// ContainerLogs streams container logs via SSE.
// GET /containers/{id}/logs
func (h *SSEHandler) ContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		http.Error(w, "container ID is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	setSSEHeaders(w)

	topic := fmt.Sprintf("container:%s", containerID)
	ch := make(chan sse.Event, 10)
	h.hub.RegisterClient(ch, topic)
	defer h.hub.UnregisterClient(ch)

	writeEvent(w, flusher, "connected", map[string]string{
		"topic": topic,
		"id":    containerID,
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

// ContainerStats streams container stats via SSE.
// GET /containers/{id}/stats
func (h *SSEHandler) ContainerStats(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		http.Error(w, "container ID is required", http.StatusBadRequest)
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
				continue
			}
			
			var buf bytes.Buffer
			if err := partials.Stats(stats).Render(r.Context(), &buf); err != nil {
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
