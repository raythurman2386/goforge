package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"text/template"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
)

// InstanceRepository defines the interface for instance data access.
type InstanceRepository interface {
	Create(ctx context.Context, s *models.ServiceInstance) error
	GetByID(ctx context.Context, id string) (*models.ServiceInstance, error)
	ListByUserID(ctx context.Context, userID string) ([]*models.ServiceInstance, error)
	Update(ctx context.Context, s *models.ServiceInstance) error
	Delete(ctx context.Context, id string) error
}

// ContainerRepository defines the interface for container data access.
type ContainerRepository interface {
	Create(ctx context.Context, c *models.Container) error
	GetByServiceInstanceID(ctx context.Context, instanceID string) (*models.Container, error)
}

// DockerClient defines the interface for Docker operations.
type DockerClient interface {
	CreateContainer(ctx context.Context, opts docker.ContainerOptions) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
}

// InstanceService handles service instance management.
type InstanceService struct {
	instanceRepo   InstanceRepository
	containerRepo  ContainerRepository
	dockerClient   DockerClient
	catalog        *Catalog
	traefikNetwork string
	logger         *slog.Logger
}

// NewInstanceService creates a new instance service.
func NewInstanceService(
	ir InstanceRepository,
	cr ContainerRepository,
	dc DockerClient,
	c *Catalog,
	traefikNetwork string,
	logger *slog.Logger,
) *InstanceService {
	return &InstanceService{
		instanceRepo:   ir,
		containerRepo:  cr,
		dockerClient:   dc,
		catalog:        c,
		traefikNetwork: traefikNetwork,
		logger:         logger,
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

	if err = s.instanceRepo.Create(ctx, instance); err != nil {
		return nil, fmt.Errorf("failed to create instance record: %w", err)
	}

	// Prepare interpolation data
	data := map[string]interface{}{
		"Config":        config,
		"Version":       instance.Version.String,
		"InstanceID":    instance.ID,
		"ContainerName": fmt.Sprintf("goforge-service-%s", instance.ID),
	}

	// Interpolate Docker config
	image, err := s.interpolate(tmpl.Docker.Image, data)
	if err != nil {
		return nil, fmt.Errorf("failed to interpolate image: %w", err)
	}

	envVars := make([]string, 0, len(tmpl.Docker.Environment))
	for k, v := range tmpl.Docker.Environment {
		val, interpErr := s.interpolate(v, data)
		if interpErr != nil {
			return nil, fmt.Errorf("failed to interpolate env %s: %w", k, interpErr)
		}
		envVars = append(envVars, k+"="+val)
	}

	// Create container
	containerName := fmt.Sprintf("goforge-service-%s", instance.ID)
	opts := docker.ContainerOptions{
		Name:        containerName,
		Image:       image,
		Env:         envVars,
		NetworkName: s.traefikNetwork,
	}

	// Add volumes (Binds)
	for _, v := range tmpl.Docker.Volumes {
		// Use named volumes prefixed with goforge-
		volumeName := fmt.Sprintf("goforge-vol-%s-%s", instance.ID, v.Name)
		opts.Binds = append(opts.Binds, fmt.Sprintf("%s:%s", volumeName, v.Path))
	}

	dockerID, err := s.dockerClient.CreateContainer(ctx, opts)
	if err != nil {
		instance.Status = models.ServiceStatusFailed
		if updateErr := s.instanceRepo.Update(ctx, instance); updateErr != nil {
			// Using blank identifier for fmt to explicitly ignore it if needed,
			// but logging is better.
			s.logger.Error("failed to update instance status to failed", "error", updateErr, "instance_id", instance.ID)
		}
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Record container in DB
	if err = s.containerRepo.Create(ctx, &models.Container{
		ServiceInstanceID: sql.NullString{String: instance.ID, Valid: true},
		DockerID:          dockerID,
		Name:              opts.Name,
		Image:             image,
		Status:            models.ContainerStatusRunning,
	}); err != nil {
		s.logger.Error("failed to record container in DB", "error", err, "instance_id", instance.ID)
	}

	instance.Status = models.ServiceStatusRunning
	if err = s.instanceRepo.Update(ctx, instance); err != nil {
		return nil, fmt.Errorf("failed to update instance status to running: %w", err)
	}

	return instance, nil
}

func (s *InstanceService) interpolate(text string, data interface{}) (string, error) {
	tmpl, err := template.New("service").Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ListInstances retrieves all instances for a user.
func (s *InstanceService) ListInstances(ctx context.Context, userID string) ([]*models.ServiceInstance, error) {
	return s.instanceRepo.ListByUserID(ctx, userID)
}

// GetInstance retrieves a service instance by ID.
func (s *InstanceService) GetInstance(ctx context.Context, id string) (*models.ServiceInstance, error) {
	return s.instanceRepo.GetByID(ctx, id)
}

// StopInstance stops the service instance.
func (s *InstanceService) StopInstance(ctx context.Context, id string) error {
	instance, err := s.instanceRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	container, err := s.containerRepo.GetByServiceInstanceID(ctx, id)
	if err != nil {
		// If no container, just update status
		instance.Status = models.ServiceStatusStopped
		return s.instanceRepo.Update(ctx, instance)
	}

	if err := s.dockerClient.StopContainer(ctx, container.DockerID); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	instance.Status = models.ServiceStatusStopped
	return s.instanceRepo.Update(ctx, instance)
}

// StartInstance starts the service instance.
func (s *InstanceService) StartInstance(ctx context.Context, id string) error {
	instance, err := s.instanceRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	container, err := s.containerRepo.GetByServiceInstanceID(ctx, id)
	if err != nil {
		return fmt.Errorf("container not found: %w", err)
	}

	if err := s.dockerClient.StartContainer(ctx, container.DockerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	instance.Status = models.ServiceStatusRunning
	return s.instanceRepo.Update(ctx, instance)
}

// RestartInstance restarts the service instance.
func (s *InstanceService) RestartInstance(ctx context.Context, id string) error {
	instance, err := s.instanceRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	container, err := s.containerRepo.GetByServiceInstanceID(ctx, id)
	if err != nil {
		return fmt.Errorf("container not found: %w", err)
	}

	if err := s.dockerClient.RestartContainer(ctx, container.DockerID); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}

	instance.Status = models.ServiceStatusRunning
	return s.instanceRepo.Update(ctx, instance)
}

// DeleteInstance stops and removes a service instance.
func (s *InstanceService) DeleteInstance(ctx context.Context, id string) error {
	// Attempt to stop and remove container first
	container, err := s.containerRepo.GetByServiceInstanceID(ctx, id)
	if err == nil && container != nil {
		if stopErr := s.dockerClient.StopContainer(ctx, container.DockerID); stopErr != nil {
			// Log error but continue with DB deletion?
			// Ideally we want to ensure cleanup, but if container is gone/invalid, we should proceed.
			// For now, let's return error if we can't clean up, or maybe log it.
			// Since we don't have logger yet, we'll return error wrapped.
			// But wait, StopContainer tries to stop AND remove.
			s.logger.Warn("failed to stop/remove container", "docker_id", container.DockerID, "error", stopErr)
		}
	}

	return s.instanceRepo.Delete(ctx, id)
}

// UpdateInstance updates a service instance.
func (s *InstanceService) UpdateInstance(ctx context.Context, instance *models.ServiceInstance) error {
	return s.instanceRepo.Update(ctx, instance)
}

// GetContainer retrieves the container for a service instance.
func (s *InstanceService) GetContainer(ctx context.Context, instanceID string) (*models.Container, error) {
	return s.containerRepo.GetByServiceInstanceID(ctx, instanceID)
}
