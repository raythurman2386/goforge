package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// InstanceService handles service instance management.
type InstanceService struct {
	instanceRepo  *repositories.ServiceInstanceRepository
	containerRepo *repositories.ContainerRepository
	dockerClient  *docker.Client
	catalog       *Catalog
}

// NewInstanceService creates a new instance service.
func NewInstanceService(
	ir *repositories.ServiceInstanceRepository,
	cr *repositories.ContainerRepository,
	dc *docker.Client,
	c *Catalog,
) *InstanceService {
	return &InstanceService{
		instanceRepo:  ir,
		containerRepo: cr,
		dockerClient:  dc,
		catalog:       c,
	}
}

// DeployInstance deploys a new service instance.
func (s *InstanceService) DeployInstance(ctx context.Context, userID, tmplName string, config map[string]interface{}) (*models.ServiceInstance, error) {
	tmpl, err := s.catalog.GetTemplate(tmplName)
	if err != nil {
		return nil, err
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	name, ok := config["name"].(string)
	if !ok {
		return nil, fmt.Errorf("config missing name")
	}

	instance := &models.ServiceInstance{
		UserID:       userID,
		TemplateName: tmplName,
		Name:         name,
		Version:      sql.NullString{String: tmpl.DefaultVersion, Valid: true},
		Config:       configJSON,
		Status:       models.ServiceStatusPending,
	}

	if err := s.instanceRepo.Create(ctx, instance); err != nil {
		return nil, fmt.Errorf("failed to create instance record: %w", err)
	}

	// TODO: Actually deploy container (async)
	// For now, we'll just update status to running to simulate
	instance.Status = models.ServiceStatusRunning
	if err := s.instanceRepo.Update(ctx, instance); err != nil {
		return nil, err
	}

	return instance, nil
}

// ListInstances retrieves all instances for a user.
func (s *InstanceService) ListInstances(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
	return s.instanceRepo.ListByUserID(ctx, userID)
}

// GetInstance retrieves a service instance by ID.
func (s *InstanceService) GetInstance(ctx context.Context, id string) (*models.ServiceInstance, error) {
	return s.instanceRepo.GetByID(ctx, id)
}

// DeleteInstance stops and removes a service instance.
func (s *InstanceService) DeleteInstance(ctx context.Context, id string) error {
	// TODO: Get associated container and stop it
	// For now, just delete the record
	return s.instanceRepo.Delete(ctx, id)
}

// UpdateInstance updates a service instance.
func (s *InstanceService) UpdateInstance(ctx context.Context, instance *models.ServiceInstance) error {
	return s.instanceRepo.Update(ctx, instance)
}
