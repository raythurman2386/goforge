package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// OrphanReconcileInterval limits background Docker scans after the startup pass.
const OrphanReconcileInterval = 15 * time.Minute

// ReconcileOrphans removes managed app containers whose project or environment
// labels no longer resolve. Unlabeled one-click services are deliberately kept.
func (s *Service) ReconcileOrphans(ctx context.Context) error {
	if !runtimeAvailable(s.dockerClient) {
		return nil
	}
	containers, err := s.dockerClient.ListContainers(ctx, true)
	if err != nil {
		return fmt.Errorf("list managed containers: %w", err)
	}
	var errs []error
	for i := range containers {
		c := &containers[i]
		projectID := c.Labels[docker.ProjectLabelKey]
		envID := c.Labels[docker.EnvironmentLabelKey]
		if projectID == "" && envID == "" {
			continue
		}
		orphan, err := s.isOrphan(ctx, projectID, envID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !orphan {
			continue
		}
		if err := s.removeContainer(ctx, c.ID); err != nil {
			errs = append(errs, fmt.Errorf("remove orphan %s: %w", c.ID, err))
			continue
		}
		s.auditOrphanRemoval(ctx, c.ID, projectID, envID)
	}
	return errors.Join(errs...)
}

func (s *Service) isOrphan(ctx context.Context, projectID, envID string) (bool, error) {
	projectExists, err := recordExists(ctx, projectID, s.projectRepo.GetByID)
	if err != nil {
		return false, fmt.Errorf("look up project %s: %w", projectID, err)
	}
	envExists, err := recordExists(ctx, envID, s.environmentRepo.GetByID)
	if err != nil {
		return false, fmt.Errorf("look up environment %s: %w", envID, err)
	}
	if !projectExists || !envExists {
		return true, nil
	}
	return false, nil
}

func recordExists[T any](ctx context.Context, id string, get func(context.Context, string) (T, error)) (bool, error) {
	if id == "" {
		return false, nil
	}
	_, err := get(ctx, id)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, errRecordNotFound) {
		return false, nil
	}
	return false, err
}

func (s *Service) auditOrphanRemoval(ctx context.Context, dockerID, projectID, envID string) {
	if s.audit == nil {
		return
	}
	details, err := json.Marshal(map[string]interface{}{
		"resource_ref": dockerID, "project_id": projectID, "environment_id": envID,
		"via": models.AuditViaSystem, "reason": "project or environment no longer exists",
	})
	if err != nil {
		slog.Default().Error("failed to marshal orphan audit", "error", err)
		return
	}
	entry := &models.AuditLog{
		ID: uuid.NewString(), Action: models.AuditActionContainerOrphanRemove,
		ResourceType: models.ResourceTypeContainer, Details: details, CreatedAt: time.Now(),
	}
	if err := s.audit.Create(ctx, entry); err != nil {
		slog.Default().Error("failed to audit orphan container removal", "error", err)
	}
}

// RunOrphanReconcile scans at startup and periodically until shutdown.
func (s *Service) RunOrphanReconcile(ctx context.Context, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	run := func() {
		if err := s.ReconcileOrphans(ctx); err != nil && ctx.Err() == nil {
			log.Error("orphan container reconcile incomplete", "error", err)
		}
	}
	run()
	ticker := time.NewTicker(OrphanReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
