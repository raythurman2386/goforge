package handlers

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/auth"
	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/web/templates/pages/containers"
)

// ContainerHandler handles container management HTTP requests.
type ContainerHandler struct {
	containerRepo *repositories.ContainerRepository
	dockerClient  *docker.Client
	logger        *slog.Logger
}

// NewContainerHandler creates a new container handler.
func NewContainerHandler(cr *repositories.ContainerRepository, dc *docker.Client, logger *slog.Logger) *ContainerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ContainerHandler{
		containerRepo: cr,
		dockerClient:  dc,
		logger:        logger,
	}
}

// ListAPI returns a list of containers as JSON for HTMX.
func (h *ContainerHandler) ListAPI(w http.ResponseWriter, r *http.Request) {
	// Fetch all tracked containers from DB
	trackedContainers, err := h.containerRepo.ListAll(r.Context())
	if err != nil {
		h.logger.Error("failed to list containers", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to list containers")
		return
	}

	// Fetch all containers from Docker
	dockerContainers, err := h.dockerClient.ListContainers(r.Context(), true)
	if err != nil {
		h.logger.Error("failed to list docker containers", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "failed to list docker containers")
		return
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

	w.Header().Set("HX-Trigger", "containerRestarted")
	w.WriteHeader(http.StatusOK)
}
