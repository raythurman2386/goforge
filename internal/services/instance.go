package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	Delete(ctx context.Context, id string) error
}

// DockerClient defines the interface for Docker operations.
type DockerClient interface {
	CreateContainer(ctx context.Context, opts docker.ContainerOptions) (string, error)
	EnsureImage(ctx context.Context, image string) error
	PullImage(ctx context.Context, image string) error
	ImageExists(ctx context.Context, image string) (bool, error)
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

// CloudflareProxyMode reports whether services are routed through a Cloudflare Tunnel.
func (s *InstanceService) CloudflareProxyMode() bool {
	return s.cloudflareProxyMode
}

// DefaultServiceDomain is the Domain prefilled on the configure form. Outside
// Cloudflare proxy mode it is <name>.localhost so a local deploy works as-is.
// In proxy mode a .localhost host can never be reached from outside, and there
// is no configured base domain to build a public host from, so it is empty.
func DefaultServiceDomain(templateName string, cloudflareProxyMode bool) string {
	if cloudflareProxyMode {
		return ""
	}
	return templateName + ".localhost"
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

	spec, err := s.buildContainerSpec(tmpl, instance, config)
	if err != nil {
		return nil, err
	}

	instanceID := instance.ID
	image := spec.opts.Image
	logAttrs := []any{"instance_id", instanceID, "template", tmplName, "image", image, "domain", domain}
	s.logger.Info("service deploy requested", append(logAttrs, "name", name)...)

	// Run deployment in background
	// We do not pass ctx to the goroutine because it is a request context that
	// will be canceled when the HTTP handler returns.
	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := s.ensureServiceImage(bgCtx, image, pullPreferRemote, logAttrs); err != nil {
			s.logger.Error("failed to pull service image", append(logAttrs, "error", err)...)
			s.setInstanceStatus(bgCtx, instanceID, models.ServiceStatusFailed)
			return
		}

		s.startServiceContainer(bgCtx, tmpl, spec, nil, logAttrs)
	}()

	return instance, nil
}

// ErrInstanceBusy is returned when a config edit is requested while the
// instance is still deploying or being recreated.
var ErrInstanceBusy = errors.New("service instance is still deploying; wait for it to finish")

// VolumeName returns the Docker named volume for one template volume of an
// instance. It depends only on the instance ID and the template volume name,
// so recreating the container for the same instance mounts the same data.
func VolumeName(instanceID, volume string) string {
	return fmt.Sprintf("goforge-vol-%s-%s", instanceID, volume)
}

// containerSpec is everything needed to create a service container for an instance.
type containerSpec struct {
	instanceID string
	opts       docker.ContainerOptions
	data       map[string]interface{}
}

// buildContainerSpec renders the template for instance with config. Deploy and
// config edits share it so a recreated container gets the same name, network,
// labels, health check, and named volumes as the original.
func (s *InstanceService) buildContainerSpec(tmpl *ServiceTemplate, instance *models.ServiceInstance, config map[string]interface{}) (*containerSpec, error) {
	domain := ""
	if instance.Domain.Valid {
		domain = instance.Domain.String
	}
	containerName := fmt.Sprintf("goforge-service-%s", instance.ID)

	data := map[string]interface{}{
		"Config":        config,
		"Version":       instance.Version.String,
		"InstanceID":    instance.ID,
		"ContainerName": containerName,
		"Domain":        domain,
		"Protocol":      publicProtocol(domain),
	}

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

	nanoCPUs, memory, err := tmpl.Resources.Limits()
	if err != nil {
		return nil, fmt.Errorf("template %s resources: %w", tmpl.Name, err)
	}

	opts := docker.ContainerOptions{
		Name:          containerName,
		Image:         image,
		Env:           envVars,
		NetworkName:   s.traefikNetwork,
		RestartPolicy: docker.RestartUnlessStopped,
		Memory:        memory,
		NanoCPUs:      nanoCPUs,
	}

	// Determine the primary HTTP port from the template
	servicePort := 0
	if len(tmpl.Docker.Ports) > 0 {
		servicePort = tmpl.Docker.Ports[0].Container
	}

	// Always mark the container as managed. Routing labels are added only when a domain is set.
	labels := proxy.GenerateServiceLabels(instance.ID, domain, s.traefikNetwork, servicePort, s.cloudflareProxyMode)
	opts.Labels = labels.ToMap()

	if hc := buildHealthCheck(tmpl.Docker.HealthCheck); hc != nil {
		opts.HealthCheck = hc
	}

	// Named volumes keyed by instance ID. Never derive these from anything an
	// edit can change, or a recreate would mount empty volumes.
	for _, v := range tmpl.Docker.Volumes {
		opts.Binds = append(opts.Binds, fmt.Sprintf("%s:%s", VolumeName(instance.ID, v.Name), v.Path))
	}

	return &containerSpec{instanceID: instance.ID, opts: opts, data: data}, nil
}

// startServiceContainer creates the container described by spec, records it,
// and marks the instance running. When replaced is non-nil its DB row is
// removed once the new container exists.
func (s *InstanceService) startServiceContainer(ctx context.Context, tmpl *ServiceTemplate, spec *containerSpec, replaced *models.Container, logAttrs []any) {
	instanceID := spec.instanceID

	dockerID, err := s.dockerClient.CreateContainer(ctx, spec.opts)
	if err != nil {
		s.logger.Error("failed to create container", "error", err, "instance_id", instanceID)
		s.setInstanceStatus(ctx, instanceID, models.ServiceStatusFailed)
		return
	}
	// CreateContainer creates and starts the container in one call.
	s.logger.Info("service container created and started", append(logAttrs, "container", spec.opts.Name, "docker_id", dockerID)...)

	if replaced != nil {
		if delErr := s.containerRepo.Delete(ctx, replaced.ID); delErr != nil {
			s.logger.Warn("failed to delete replaced container record", "error", delErr, "instance_id", instanceID, "container_id", replaced.ID)
		}
	}

	if err = s.containerRepo.Create(ctx, &models.Container{
		ServiceInstanceID: sql.NullString{String: instanceID, Valid: true},
		DockerID:          dockerID,
		Name:              spec.opts.Name,
		Image:             spec.opts.Image,
		Status:            models.ContainerStatusRunning,
	}); err != nil {
		s.logger.Error("failed to record container in DB", "error", err, "instance_id", instanceID)
	}

	// Fetch a fresh copy to avoid racing with the returned pointer
	inst, err := s.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		s.logger.Error("failed to fetch instance for status update", "error", err, "instance_id", instanceID)
		return
	}

	inst.Status = models.ServiceStatusRunning

	// Render connection string templates and store in ConnectionInfo
	if connJSON := s.renderConnectionInfo(tmpl, spec.data, instanceID); connJSON != nil {
		inst.ConnectionInfo = connJSON
	}

	if err = s.instanceRepo.Update(ctx, inst); err != nil {
		s.logger.Error("failed to update instance status to running", "error", err, "instance_id", instanceID)
		return
	}
	s.logger.Info("service running", logAttrs...)
}

// UpdateInstanceConfig applies edited config to a deployed instance and
// recreates its container. The instance ID does not change, so the new
// container mounts the same goforge-vol-<instanceID>-<name> volumes and keeps
// its data. Only the container is removed; volumes are never touched.
//
// updates holds raw form values. Keys other than name, domain, and the
// template's config fields are ignored. An empty password field keeps the
// stored value.
func (s *InstanceService) UpdateInstanceConfig(ctx context.Context, id string, updates map[string]interface{}) (*models.ServiceInstance, error) {
	instance, err := s.instanceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if instance.Status == models.ServiceStatusPending || instance.Status == models.ServiceStatusStarting {
		return nil, ErrInstanceBusy
	}

	tmpl, err := s.catalog.GetTemplate(instance.TemplateName)
	if err != nil {
		return nil, err
	}

	current, err := instance.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to parse stored config: %w", err)
	}

	config := mergeEditedConfig(tmpl, current, updates)
	if genErr := fillGeneratedConfig(tmpl, config); genErr != nil {
		return nil, genErr
	}

	name, ok := config["name"].(string)
	if !ok || name == "" {
		return nil, errors.New("config missing name")
	}
	var domain string
	if d, ok := config["domain"].(string); ok {
		domain = d
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	instance.Name = name
	instance.Config = configJSON
	instance.Domain = sql.NullString{String: domain, Valid: domain != ""}

	// Render before touching anything so a bad template leaves the running container alone.
	spec, err := s.buildContainerSpec(tmpl, instance, config)
	if err != nil {
		return nil, err
	}

	prevStatus := instance.Status
	instance.Status = models.ServiceStatusPending
	if err = s.instanceRepo.Update(ctx, instance); err != nil {
		return nil, fmt.Errorf("failed to save instance config: %w", err)
	}

	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.recreateContainer(bgCtx, tmpl, spec, pullPreferRemote, prevStatus)
	}()

	return instance, nil
}

// UpdateInstanceImage pulls the instance's image again and recreates its
// container with the stored config. It is how a service on a moving tag such
// as latest picks up a newer upstream image. The instance ID does not change,
// so the new container mounts the same goforge-vol-<instanceID>-<name>
// volumes. The pull must succeed: if the registry cannot be reached, the
// running container is left alone instead of being recreated on the old image.
func (s *InstanceService) UpdateInstanceImage(ctx context.Context, id string) (*models.ServiceInstance, error) {
	instance, err := s.instanceRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if instance.Status == models.ServiceStatusPending || instance.Status == models.ServiceStatusStarting {
		return nil, ErrInstanceBusy
	}

	tmpl, err := s.catalog.GetTemplate(instance.TemplateName)
	if err != nil {
		return nil, err
	}

	config, err := instance.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to parse stored config: %w", err)
	}

	// Render before touching anything so a bad template leaves the running container alone.
	spec, err := s.buildContainerSpec(tmpl, instance, config)
	if err != nil {
		return nil, err
	}

	prevStatus := instance.Status
	instance.Status = models.ServiceStatusPending
	if err = s.instanceRepo.Update(ctx, instance); err != nil {
		return nil, fmt.Errorf("failed to save instance status: %w", err)
	}
	s.logger.Info("service image update requested", "instance_id", instance.ID, "template", tmpl.Name, "image", spec.opts.Image)

	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		s.recreateContainer(bgCtx, tmpl, spec, pullRequired, prevStatus)
	}()

	return instance, nil
}

// recreateContainer gets the image for spec, then stops and removes the
// instance's current container (not its named volumes) and starts a new one
// from spec. The image comes first so a failed pull leaves the old container
// running; the instance then goes back to prevStatus.
func (s *InstanceService) recreateContainer(ctx context.Context, tmpl *ServiceTemplate, spec *containerSpec, policy imagePullPolicy, prevStatus models.ServiceInstanceStatus) {
	domain := ""
	if d, ok := spec.data["Domain"].(string); ok {
		domain = d
	}
	logAttrs := []any{"instance_id", spec.instanceID, "template", tmpl.Name, "image", spec.opts.Image, "domain", domain}
	if err := s.ensureServiceImage(ctx, spec.opts.Image, policy, logAttrs); err != nil {
		s.logger.Error("failed to pull service image; existing container left in place", append(logAttrs, "error", err)...)
		s.setInstanceStatus(ctx, spec.instanceID, prevStatus)
		return
	}

	old, err := s.containerRepo.GetByServiceInstanceID(ctx, spec.instanceID)
	if err != nil || old == nil {
		s.logger.Warn("no container record for instance; creating a new container", "instance_id", spec.instanceID, "error", err)
		old = nil
	}

	if old != nil {
		if stopErr := s.dockerClient.StopContainer(ctx, old.DockerID); stopErr != nil {
			s.logger.Warn("failed to stop container", "docker_id", old.DockerID, "error", stopErr)
		}
		// Docker keeps named volumes on container removal; RemoveVolumes only
		// drops anonymous ones. The goforge-vol-* volumes stay for the new container.
		if removeErr := s.dockerClient.RemoveContainer(ctx, old.DockerID); removeErr != nil {
			s.logger.Warn("failed to remove container", "docker_id", old.DockerID, "error", removeErr)
		}
	}

	s.startServiceContainer(ctx, tmpl, spec, old, logAttrs)
}

// mergeEditedConfig returns current with the editable fields from updates
// applied. Editable fields are name, domain, and the template's config
// fields. A blank password field keeps its stored value.
func mergeEditedConfig(tmpl *ServiceTemplate, current, updates map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(current))
	for k, v := range current {
		merged[k] = v
	}

	setString := func(key string, keepIfBlank bool) {
		raw, ok := updates[key]
		if !ok {
			return
		}
		val := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if val == "" && keepIfBlank {
			return
		}
		merged[key] = val
	}

	setString("name", true)
	setString("domain", false)
	for _, field := range tmpl.Config {
		if field.Name == "name" || field.Name == "domain" {
			continue
		}
		setString(field.Name, field.Type == "password")
	}
	return merged
}

// imagePullPolicy says what a service operation does when it cannot pull its image.
type imagePullPolicy int

const (
	// pullPreferRemote pulls tag references and falls back to a local copy
	// when the registry cannot be reached. Deploys and config edits use it.
	pullPreferRemote imagePullPolicy = iota
	// pullRequired pulls tag references and fails when the pull fails. The
	// Update action uses it so it never quietly recreates on the old image.
	pullRequired
)

// ensureServiceImage makes the service image available and logs pull vs
// cache hit. A tag (including an implied latest) can move upstream, so tag
// references are pulled every time; an unchanged tag costs only a manifest
// check. Digest references (name@sha256:...) never change and are pulled
// only when missing.
func (s *InstanceService) ensureServiceImage(ctx context.Context, image string, policy imagePullPolicy, logAttrs []any) error {
	if docker.IsDigestReference(image) {
		exists, err := s.dockerClient.ImageExists(ctx, image)
		if err != nil {
			s.logger.Warn("failed to check for service image", append(logAttrs, "error", err)...)
		}
		if exists {
			s.logger.Info("service image present locally", logAttrs...)
			return nil
		}
		s.logger.Info("pulling service image", logAttrs...)
		start := time.Now()
		if err := s.dockerClient.EnsureImage(ctx, image); err != nil {
			return err
		}
		s.logger.Info("pulled service image", append(logAttrs, "duration", time.Since(start).Round(time.Millisecond).String())...)
		return nil
	}

	s.logger.Info("pulling service image", logAttrs...)
	start := time.Now()
	pullErr := s.dockerClient.PullImage(ctx, image)
	if pullErr == nil {
		s.logger.Info("pulled service image", append(logAttrs, "duration", time.Since(start).Round(time.Millisecond).String())...)
		return nil
	}
	if policy == pullRequired {
		return pullErr
	}

	exists, err := s.dockerClient.ImageExists(ctx, image)
	if err != nil {
		s.logger.Warn("failed to check for service image", append(logAttrs, "error", err)...)
	}
	if !exists {
		return pullErr
	}
	s.logger.Warn("service image pull failed; using local image", append(logAttrs, "error", pullErr)...)
	return nil
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
