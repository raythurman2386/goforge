package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/containers"
)

// ContainerHandler handles container management HTTP requests.
type ContainerHandler struct {
	containerRepo       *repositories.ContainerRepository
	serviceInstanceRepo *repositories.ServiceInstanceRepository
	dockerClient        *docker.Client
	logger              *slog.Logger
	audit               uiAudit
}

// WithAudit records container stop and restart in the audit log (via=ui).
// The Docker id is not a UUID, so it lands in details.resource_ref.
func (h *ContainerHandler) WithAudit(rec UIAuditRecorder) *ContainerHandler {
	h.audit = uiAudit{rec: rec}
	return h
}

// NewContainerHandler creates a new container handler.
func NewContainerHandler(cr *repositories.ContainerRepository, sir *repositories.ServiceInstanceRepository, dc *docker.Client, logger *slog.Logger) *ContainerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ContainerHandler{
		containerRepo:       cr,
		serviceInstanceRepo: sir,
		dockerClient:        dc,
		logger:              logger,
	}
}

// ListAPI returns a list of containers as JSON for HTMX.
func (h *ContainerHandler) ListAPI(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(auth.UserContextKey).(*models.User)
	if !ok {
		RenderError(w, r, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Fetch tracked containers scoped to the authenticated user
	trackedContainers, err := h.containerRepo.ListByUserID(r.Context(), user.ID)
	if err != nil {
		h.logger.Error("failed to list containers", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to list containers")
		return
	}

	// Build a set of tracked Docker IDs so we can filter Docker containers
	trackedDockerIDs := make(map[string]struct{}, len(trackedContainers))
	for _, tc := range trackedContainers {
		trackedDockerIDs[tc.DockerID] = struct{}{}
	}

	// Fetch all containers from Docker, then filter to only those owned by user
	allDockerContainers, err := h.dockerClient.ListContainers(r.Context(), true)
	if err != nil {
		h.logger.Error("failed to list docker containers", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to list docker containers")
		return
	}

	var dockerContainers []container.Summary
	for i := range allDockerContainers {
		if _, ok := trackedDockerIDs[allDockerContainers[i].ID]; ok {
			dockerContainers = append(dockerContainers, allDockerContainers[i])
		}
	}

	component := containers.ListAPI(trackedContainers, dockerContainers)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render container list")
	}
}

// List renders the container list page.
func (h *ContainerHandler) List(w http.ResponseWriter, r *http.Request) {
	csrfToken := auth.GetCSRFToken(r)

	component := containers.List(csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render container list")
	}
}

// Detail renders the container detail page.
func (h *ContainerHandler) Detail(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		RenderError(w, r, http.StatusBadRequest, "missing container ID")
		return
	}

	if !h.verifyContainerOwnership(r.Context(), w, r, containerID) {
		return
	}

	csrfToken := auth.GetCSRFToken(r)
	component := containers.Detail(containerID, csrfToken)
	if err := component.Render(r.Context(), w); err != nil {
		RenderError(w, r, http.StatusInternalServerError, "failed to render container detail")
	}
}

// Stop stops a container.
func (h *ContainerHandler) Stop(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		RenderError(w, r, http.StatusBadRequest, "missing container ID")
		return
	}

	ctx := r.Context()

	// Verify ownership: tracked container must belong to current user
	if !h.verifyContainerOwnership(ctx, w, r, containerID) {
		return
	}

	// Stop the container
	if err := h.dockerClient.StopContainer(ctx, containerID); err != nil {
		h.logger.Error("failed to stop container", "container_id", containerID, "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to stop container")
		return
	}

	// Update status in DB if we have it tracked
	if trackedContainer, err := h.containerRepo.GetByDockerID(ctx, containerID); err == nil {
		if err := h.containerRepo.UpdateStatus(ctx, trackedContainer.ID, models.ContainerStatusExited); err != nil {
			h.logger.Warn("failed to update container status in DB", "container_id", trackedContainer.ID, "error", err)
		}
	}

	h.audit.record(r, models.AuditActionContainerStop, models.ResourceTypeContainer, containerID, nil)

	w.Header().Set("HX-Trigger", "containerStopped")
	w.WriteHeader(http.StatusOK)
}

// Restart restarts a container.
func (h *ContainerHandler) Restart(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if containerID == "" {
		RenderError(w, r, http.StatusBadRequest, "missing container ID")
		return
	}

	ctx := r.Context()

	// Verify ownership: tracked container must belong to current user
	if !h.verifyContainerOwnership(ctx, w, r, containerID) {
		return
	}

	// Restart the container
	if err := h.dockerClient.RestartContainer(ctx, containerID); err != nil {
		h.logger.Error("failed to restart container", "container_id", containerID, "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to restart container")
		return
	}

	// Update status in DB if we have it tracked
	if trackedContainer, err := h.containerRepo.GetByDockerID(ctx, containerID); err == nil {
		if err := h.containerRepo.UpdateStatus(ctx, trackedContainer.ID, models.ContainerStatusRunning); err != nil {
			h.logger.Warn("failed to update container status in DB", "container_id", trackedContainer.ID, "error", err)
		}
	}

	h.audit.record(r, models.AuditActionContainerRestart, models.ResourceTypeContainer, containerID, nil)

	w.Header().Set("HX-Trigger", "containerRestarted")
	w.WriteHeader(http.StatusOK)
}

// verifyContainerOwnership checks that the Docker container belongs to the
// authenticated user via a service instance or deployment → environment → project.
func (h *ContainerHandler) verifyContainerOwnership(ctx context.Context, w http.ResponseWriter, _ *http.Request, dockerID string) bool {
	user, ok := ctx.Value(auth.UserContextKey).(*models.User)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}

	owned, err := h.containerRepo.OwnedByUser(ctx, dockerID, user.ID)
	if err != nil || !owned {
		http.Error(w, "access denied", http.StatusForbidden)
		return false
	}
	return true
}
