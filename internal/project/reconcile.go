package project

import (
	"context"
	"database/sql"
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
const (
	OrphanReconcileInterval = 15 * time.Minute
	OrphanReconcileTimeout  = 2 * time.Minute
)

// ReconcileOrphans removes managed app containers whose project or environment
// labels no longer resolve. Outstanding cleanup intents are retried even when
// Docker no longer lists the object. Unlabeled one-click services are kept.
func (s *Service) ReconcileOrphans(ctx context.Context) error {
	candidates, errs := s.reconcileCandidates(ctx)
	for projectID, containers := range groupByProject(candidates) {
		err := s.lifecycleCoordinator().Exclusive(projectID, func() error {
			return s.reconcileProject(ctx, containers)
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) reconcileCandidates(ctx context.Context) (map[string]labeledContainer, []error) {
	candidates := make(map[string]labeledContainer)
	var errs []error
	if runtimeAvailable(s.dockerClient) {
		if err := s.addRuntimeCandidates(ctx, candidates); err != nil {
			errs = append(errs, err)
		}
	}
	pending, err := s.pendingCleanup(ctx)
	if err != nil {
		errs = append(errs, err)
	}
	for _, c := range pending {
		current := candidates[c.id]
		if current.id == "" {
			current = c
		}
		current.retry, current.reason = true, c.reason
		candidates[c.id] = current
	}
	return candidates, errs
}

func (s *Service) addRuntimeCandidates(ctx context.Context, candidates map[string]labeledContainer) error {
	containers, err := s.dockerClient.ListContainers(ctx, true)
	if err != nil {
		return fmt.Errorf("list managed containers: %w", err)
	}
	for i := range containers {
		c := &containers[i]
		projectID := c.Labels[docker.ProjectLabelKey]
		envID := c.Labels[docker.EnvironmentLabelKey]
		if projectID == "" && envID == "" || c.Labels[docker.ManagedLabelKey] != docker.ManagedLabelValue {
			continue
		}
		candidates[c.ID] = labeledContainer{id: c.ID, projectID: projectID, envID: envID}
	}
	return nil
}

func groupByProject(candidates map[string]labeledContainer) map[string][]labeledContainer {
	groups := make(map[string][]labeledContainer)
	for _, c := range candidates {
		groups[c.projectID] = append(groups[c.projectID], c)
	}
	return groups
}

func (s *Service) reconcileProject(ctx context.Context, containers []labeledContainer) error {
	var errs []error
	for _, c := range containers {
		orphan, audit, err := s.cleanupAllowed(ctx, c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !orphan {
			continue
		}
		if audit {
			c.reason = models.ContainerCleanupOrphan
		}
		owner := s.orphanOwner(ctx, c)
		if err := s.removeContainer(ctx, c); err != nil {
			errs = append(errs, fmt.Errorf("remove orphan %s: %w", c.id, err))
			continue
		}
		if audit {
			s.auditOrphanRemoval(ctx, c.id, c.projectID, c.envID, owner)
		}
	}
	return errors.Join(errs...)
}

func (s *Service) pendingCleanup(ctx context.Context) (map[string]labeledContainer, error) {
	pending := make(map[string]labeledContainer)
	if s.cleanupStore == nil {
		return pending, nil
	}
	rows, err := s.cleanupStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending container cleanup: %w", err)
	}
	for _, c := range rows {
		pending[c.DockerID] = labeledContainer{id: c.DockerID, projectID: c.ProjectID.String, envID: c.EnvironmentID.String, retry: true, reason: c.Reason}
	}
	return pending, nil
}

// cleanupAllowed distinguishes a parent that never existed from a pending host
// removal that must not produce a duplicate automatic-orphan audit.
func (s *Service) cleanupAllowed(ctx context.Context, c labeledContainer) (orphan, audit bool, err error) {
	projectExists, err := recordExists(ctx, c.projectID, s.projectRepo.GetByID)
	if err != nil {
		return false, false, fmt.Errorf("look up project %s: %w", c.projectID, err)
	}
	envExists, err := recordExists(ctx, c.envID, s.environmentRepo.GetByID)
	if err != nil {
		return false, false, fmt.Errorf("look up environment %s: %w", c.envID, err)
	}
	if !projectExists || !envExists {
		return true, c.reason != models.ContainerCleanupDelete, nil
	}
	// A live parent permits only an already-requested cleanup (rollback record
	// failure, retirement failure, or a prior failed remove), never a new audit.
	return c.retry, false, nil
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

func (s *Service) orphanOwner(ctx context.Context, c labeledContainer) sql.NullString {
	if s.projectRepo != nil && c.projectID != "" {
		if project, err := s.projectRepo.GetByID(ctx, c.projectID); err == nil {
			return sql.NullString{String: project.UserID, Valid: project.UserID != ""}
		}
	}
	if s.auditOwner == nil {
		return sql.NullString{}
	}
	owner, err := s.auditOwner.Owner(ctx, c.id)
	if err != nil {
		return sql.NullString{}
	}
	return owner
}

func (s *Service) auditOrphanRemoval(ctx context.Context, dockerID, projectID, envID string, userID sql.NullString) {
	if s.audit == nil {
		return
	}
	details, err := json.Marshal(map[string]interface{}{
		"resource_ref": dockerID, "project_id": projectID, "environment_id": envID,
		"via": models.AuditViaSystem, "reason": "project or environment no longer exists",
	})
	if err != nil {
		return
	}
	log := &models.AuditLog{
		ID: uuid.NewString(), UserID: userID, Action: models.AuditActionContainerOrphanRemove,
		ResourceType: models.ResourceTypeContainer, Details: details,
	}
	if err := s.audit.Create(ctx, log); err != nil {
		slog.Default().Error("failed to audit orphan container removal", "error", err)
	}
}

// RunOrphanReconcile scans at startup and periodically until shutdown.
func (s *Service) RunOrphanReconcile(ctx context.Context, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	run := func() {
		passCtx, cancel := context.WithTimeout(ctx, OrphanReconcileTimeout)
		defer cancel()
		if err := s.ReconcileOrphans(passCtx); err != nil && ctx.Err() == nil {
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
