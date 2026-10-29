package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/docker/docker/api/types/container"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/proxy"
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
	RemoveContainer(ctx context.Context, id string) error
	RestartContainer(ctx context.Context, id string) error
	CleanupSystem(ctx context.Context) error
}

// InstanceService handles service instance management.
type InstanceService struct {
	instanceRepo        InstanceRepository
	containerRepo       ContainerRepository
	dockerClient        DockerClient
	catalog             *Catalog
	traefikNetwork      string
	cloudflareProxyMode bool
	logger              *slog.Logger
}

// NewInstanceService creates a new instance service.
func NewInstanceService(
	ir InstanceRepository,
	cr ContainerRepository,
	dc DockerClient,
	c *Catalog,
	traefikNetwork string,
	cloudflareProxyMode bool,
	logger *slog.Logger,
) *InstanceService {
	return &InstanceService{
		instanceRepo:        ir,
		containerRepo:       cr,
		dockerClient:        dc,
		catalog:             c,
		traefikNetwork:      traefikNetwork,
		cloudflareProxyMode: cloudflareProxyMode,
		logger:              logger,
	}
}

// publicProtocol is the scheme a browser uses to reach domain. Cloudflare proxy
// mode still means HTTPS for the visitor: Cloudflare ends TLS and forwards HTTP
// to Traefik. Only localhost domains and services without a domain use HTTP.
func publicProtocol(domain string) string {
	if domain == "" || proxy.IsLocalhostDomain(domain) {
		return "http"
	}
	return "https"
}

// DeployInstance deploys a new service instance.
func (s *InstanceService) DeployInstance(ctx context.Context, userID, tmplName string, config map[string]interface{}) (*models.ServiceInstance, error) {
	tmpl, err := s.catalog.GetTemplate(tmplName)
	if err != nil {
		return nil, err
	}

	if genErr := fillGeneratedConfig(tmpl, config); genErr != nil {
		return nil, genErr
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	name, ok := config["name"].(string)
	if !ok {
		return nil, errors.New("config missing name")
	}

	// Extract optional domain for Traefik routing
	var domain string
	if d, ok := config["domain"].(string); ok {
		domain = d
	}

	instance := &models.ServiceInstance{
		UserID:       userID,
		TemplateName: tmplName,
		Name:         name,
		Version:      sql.NullString{String: tmpl.DefaultVersion, Valid: true},
		Config:       configJSON,
		Domain:       sql.NullString{String: domain, Valid: domain != ""},
		Status:       models.ServiceStatusPending,
	}

	if err = s.instanceRepo.Create(ctx, instance); err != nil {
		return nil, fmt.Errorf("failed to create instance record: %w", err)
	}

	// Prepare interpolation data
	containerName := fmt.Sprintf("goforge-service-%s", instance.ID)
	protocol := publicProtocol(domain)

	data := map[string]interface{}{
		"Config":        config,
		"Version":       instance.Version.String,
		"InstanceID":    instance.ID,
		"ContainerName": containerName,
		"Domain":        domain,
		"Protocol":      protocol,
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
	opts := docker.ContainerOptions{
		Name:          containerName,
		Image:         image,
		Env:           envVars,
		NetworkName:   s.traefikNetwork,
		RestartPolicy: docker.RestartUnlessStopped,
	}

	// Determine the primary HTTP port from the template
	servicePort := 0
	if len(tmpl.Docker.Ports) > 0 {
		servicePort = tmpl.Docker.Ports[0].Container
	}

	// Always mark the container as managed. Routing labels are added only when a domain is set.
	labels := proxy.GenerateServiceLabels(instance.ID, domain, s.traefikNetwork, servicePort, s.cloudflareProxyMode)
	opts.Labels = labels.ToMap()

	// Apply health check from template
	if hc := buildHealthCheck(tmpl.Docker.HealthCheck); hc != nil {
		opts.HealthCheck = hc
	}

	// Add volumes (Binds)
	for _, v := range tmpl.Docker.Volumes {
		// Use named volumes prefixed with goforge-
		volumeName := fmt.Sprintf("goforge-vol-%s-%s", instance.ID, v.Name)
		opts.Binds = append(opts.Binds, fmt.Sprintf("%s:%s", volumeName, v.Path))
	}

	// Capture immutable values needed by the background goroutine.
	instanceID := instance.ID

	// Run deployment in background
	// We do not pass ctx to the goroutine because it is a request context that
	// will be canceled when the HTTP handler returns.
	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		// Create a new context since the request context will be canceled
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		dockerID, err := s.dockerClient.CreateContainer(bgCtx, opts)
		if err != nil {
			s.logger.Error("failed to create container", "error", err, "instance_id", instanceID)
			s.setInstanceStatus(bgCtx, instanceID, models.ServiceStatusFailed)
			return
		}

		// Record container in DB
		if err = s.containerRepo.Create(bgCtx, &models.Container{
			ServiceInstanceID: sql.NullString{String: instanceID, Valid: true},
			DockerID:          dockerID,
			Name:              opts.Name,
			Image:             image,
			Status:            models.ContainerStatusRunning,
		}); err != nil {
			s.logger.Error("failed to record container in DB", "error", err, "instance_id", instanceID)
		}

		// Fetch a fresh copy to avoid racing with the returned pointer
		inst, err := s.instanceRepo.GetByID(bgCtx, instanceID)
		if err != nil {
			s.logger.Error("failed to fetch instance for status update", "error", err, "instance_id", instanceID)
			return
		}

		inst.Status = models.ServiceStatusRunning

		// Render connection string templates and store in ConnectionInfo
		if connJSON := s.renderConnectionInfo(tmpl, data, instanceID); connJSON != nil {
			inst.ConnectionInfo = connJSON
		}

		if err = s.instanceRepo.Update(bgCtx, inst); err != nil {
			s.logger.Error("failed to update instance status to running", "error", err, "instance_id", instanceID)
		}
	}()

	return instance, nil
}

// setInstanceStatus fetches the instance and updates its status.
func (s *InstanceService) setInstanceStatus(ctx context.Context, instanceID string, status models.ServiceInstanceStatus) {
	inst, err := s.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		s.logger.Error("failed to fetch instance for status update", "error", err, "instance_id", instanceID)
		return
	}
	inst.Status = status
	if err = s.instanceRepo.Update(ctx, inst); err != nil {
		s.logger.Error("failed to update instance status", "error", err, "instance_id", instanceID, "status", status)
	}
}

func (s *InstanceService) renderConnectionInfo(tmpl *ServiceTemplate, data map[string]interface{}, instanceID string) []byte {
	connInfo := make(map[string]string)
	if tmpl.Connection.Internal != "" {
		internal, interpErr := s.interpolate(tmpl.Connection.Internal, data)
		if interpErr != nil {
			s.logger.Error("failed to interpolate internal connection string", "error", interpErr, "instance_id", instanceID)
		} else {
			connInfo["internal"] = internal
		}
	}
	if tmpl.Connection.External != "" {
		external, interpErr := s.interpolate(tmpl.Connection.External, data)
		if interpErr != nil {
			s.logger.Error("failed to interpolate external connection string", "error", interpErr, "instance_id", instanceID)
		} else {
			connInfo["external"] = external
		}
	}
	if len(connInfo) > 0 {
		connJSON, marshalErr := json.Marshal(connInfo)
		if marshalErr != nil {
			s.logger.Error("failed to marshal connection info", "error", marshalErr, "instance_id", instanceID)
		} else {
			return connJSON
		}
	}
	return nil
}

// buildHealthCheck converts a template HealthCheckConfig into a Docker HealthConfig.
// Returns nil if the template has no health check test command configured.
func buildHealthCheck(hc HealthCheckConfig) *container.HealthConfig {
	if len(hc.Test) == 0 {
		return nil
	}

	interval, parseErr := time.ParseDuration(hc.Interval)
	if parseErr != nil {
		interval = 10 * time.Second
	}

	timeout, parseErr := time.ParseDuration(hc.Timeout)
	if parseErr != nil {
		timeout = 5 * time.Second
	}

	return &container.HealthConfig{
		Test:     hc.Test,
		Interval: interval,
		Timeout:  timeout,
		Retries:  hc.Retries,
	}
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

// PruneSystem cleans up unused containers and images.
func (s *InstanceService) PruneSystem(ctx context.Context) error {
	if err := s.dockerClient.CleanupSystem(ctx); err != nil {
		return fmt.Errorf("failed to prune system: %w", err)
	}
	return nil
}

// DeleteInstance stops and removes a service instance.
func (s *InstanceService) DeleteInstance(ctx context.Context, id string) error {
	// Attempt to stop and remove container first
	container, err := s.containerRepo.GetByServiceInstanceID(ctx, id)
	if err == nil && container != nil {
		if stopErr := s.dockerClient.StopContainer(ctx, container.DockerID); stopErr != nil {
			s.logger.Warn("failed to stop container", "docker_id", container.DockerID, "error", stopErr)
		}
		// Remove the container after stopping (StopContainer only stops, does not remove)
		if removeErr := s.dockerClient.RemoveContainer(ctx, container.DockerID); removeErr != nil {
			s.logger.Warn("failed to remove container", "docker_id", container.DockerID, "error", removeErr)
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
