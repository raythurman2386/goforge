package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/notify"
	"github.com/raythurman2386/goforge/internal/proxy"
)

// InstanceRepository defines the interface for instance data access.
type InstanceRepository interface {
	Create(ctx context.Context, s *models.ServiceInstance) error
	GetByID(ctx context.Context, id string) (*models.ServiceInstance, error)
	ListByUserID(ctx context.Context, userID string) ([]*models.ServiceInstance, error)
	Update(ctx context.Context, s *models.ServiceInstance) error
	// UpdateIfStatus writes s only if the stored status still equals
	// expected, as one atomic update. It returns false, writing nothing,
	// when the status has changed since the caller read it.
	UpdateIfStatus(ctx context.Context, s *models.ServiceInstance, expected models.ServiceInstanceStatus) (bool, error)
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
	// RenameContainer renames a container. A recreate that publishes host
	// ports parks the old container under another name until the new one starts.
	RenameContainer(ctx context.Context, id, newName string) error
	RestartContainer(ctx context.Context, id string) error
	CleanupSystem(ctx context.Context) error
	// WaitHealthy blocks until the container's health check passes (or, with
	// no health check, until it runs), polling every poll.
	WaitHealthy(ctx context.Context, id string, poll time.Duration) error
	// Exec runs a command in a running container and returns its exit code.
	Exec(ctx context.Context, id string, opts docker.ExecOptions) (docker.ExecResult, error)
}

// healthPollInterval is how often a post_start wait checks container health.
const healthPollInterval = 2 * time.Second

// backgroundWorkTimeout bounds a background deploy, config edit, or image
// update, including every post_start hook.
const backgroundWorkTimeout = 10 * time.Minute

// statusWriteTimeout bounds the final status write after background work,
// which gets a fresh deadline because the work may have used up its own.
const statusWriteTimeout = 10 * time.Second

// InstanceService handles service instance management.
type InstanceService struct {
	instanceRepo        InstanceRepository
	containerRepo       ContainerRepository
	dockerClient        DockerClient
	catalog             *Catalog
	traefikNetwork      string
	cloudflareProxyMode bool
	logger              *slog.Logger
	healthPoll          time.Duration
	bgTimeout           time.Duration
	audit               AuditRunStore
	notifier            notify.Notifier
}

// WithAudit makes background deploys, config edits, and image updates write
// service.run.completed or service.run.failed when they end, if the call's
// context carries an audit id (models.WithAuditID). details.audit_id is that
// id.
func (s *InstanceService) WithAudit(store AuditRunStore) *InstanceService {
	s.audit = store
	return s
}

// recordRunOutcome runs when background work on an instance ends. It
// writes service.run.<result> for auditID from the instance's final status
// and last_error (when there is an audit store and id), and sends a
// service.failed alert when the work failed (when there is a notifier).
func (s *InstanceService) recordRunOutcome(auditID, instanceID string) {
	wantAudit := auditID != "" && s.audit != nil
	if !wantAudit && s.notifier == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), statusWriteTimeout)
	defer cancel()
	out := RunOutcome{
		Run:          models.AuditRunService,
		ResourceType: models.ResourceTypeServiceInstance,
		ResourceID:   instanceID,
		Result:       models.AuditRunCompleted,
	}
	name := ""
	inst, err := s.instanceRepo.GetByID(ctx, instanceID)
	switch {
	case err != nil:
		out.Result, out.Error = models.AuditRunFailed, "could not read the service status after the change"
	default:
		name = inst.Name
		out.Status = string(inst.Status)
		if inst.LastError.Valid && inst.LastError.String != "" {
			out.Result, out.Error = models.AuditRunFailed, inst.LastError.String
		} else if inst.Status == models.ServiceStatusFailed {
			out.Result = models.AuditRunFailed
		}
	}
	if wantAudit {
		RecordRunOutcome(s.audit, s.logger, auditID, out)
	}
	// An unreadable instance (deleted mid-run) is audited but not alerted.
	if s.notifier != nil && err == nil && out.Result == models.AuditRunFailed {
		msg := out.Error
		if msg == "" {
			msg = "service status is failed"
		}
		s.notifier.Notify(ctx, notify.Event{
			Kind:     notify.EventServiceFailed,
			Resource: notify.Resource{Type: models.ResourceTypeServiceInstance, ID: instanceID, Name: name},
			Error:    msg,
			Path:     "/services/" + instanceID,
			AuditID:  auditID,
			Time:     time.Now(),
		})
	}
}

// WithNotifier makes background deploys, config edits, and image updates
// send a service.failed alert (with last_error) when they fail.
func (s *InstanceService) WithNotifier(n notify.Notifier) *InstanceService {
	s.notifier = n
	return s
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
		healthPoll:          healthPollInterval,
		bgTimeout:           backgroundWorkTimeout,
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

	if genErr := fillGeneratedConfig(tmpl, config, true); genErr != nil {
		return nil, genErr
	}
	// Reject a bad bool or select value before any record or container exists.
	if cfgErr := normalizeConfig(tmpl, config); cfgErr != nil {
		return nil, cfgErr
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

	// Reject a bad published host port before any record or container exists.
	if _, portErr := s.renderPortBindings(tmpl, s.templateData(tmpl, &models.ServiceInstance{
		Version: sql.NullString{String: tmpl.DefaultVersion, Valid: true},
		Domain:  sql.NullString{String: domain, Valid: domain != ""},
	}, config)); portErr != nil {
		return nil, portErr
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
	auditID := models.AuditIDFrom(ctx)
	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		defer s.recordRunOutcome(auditID, instanceID)
		bgCtx, cancel := context.WithTimeout(context.Background(), s.bgTimeout)
		defer cancel()

		if err := s.ensureServiceImage(bgCtx, image, pullPreferRemote, logAttrs); err != nil {
			s.logger.Error("failed to pull service image", append(logAttrs, "error", err)...)
			s.setInstanceStatus(bgCtx, instanceID, models.ServiceStatusFailed, lastErrorText("image pull failed for "+image, err, spec))
			return
		}

		s.startServiceContainer(bgCtx, tmpl, spec, nil, logAttrs)
	}()

	return instance, nil
}

// ErrInstanceBusy is returned when a config edit or image update is
// requested while the instance is still deploying or being recreated,
// including when another edit claimed the instance first (#48).
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
	hooks      []renderedHook
	// secrets are the instance's secret config values, for redacting hook output.
	secrets []string
}

// renderedHook is a post_start hook with its templates rendered.
type renderedHook struct {
	name string
	exec docker.ExecOptions
}

// buildContainerSpec renders the template for instance with config. Deploy and
// config edits share it so a recreated container gets the same name, network,
// labels, health check, and named volumes as the original.
func (s *InstanceService) buildContainerSpec(tmpl *ServiceTemplate, instance *models.ServiceInstance, config map[string]interface{}) (*containerSpec, error) {
	data := s.templateData(tmpl, instance, config)
	containerName := dataString(data, "ContainerName")
	domain := dataString(data, "Domain")

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
	for k, v := range tmpl.Docker.OptionalEnvironment {
		val, interpErr := s.interpolate(v, data)
		if interpErr != nil {
			return nil, fmt.Errorf("failed to interpolate optional env %s: %w", k, interpErr)
		}
		if val != "" {
			envVars = append(envVars, k+"="+val)
		}
	}

	cmd, err := s.renderCommand(tmpl.Docker.Command, data)
	if err != nil {
		return nil, err
	}

	hooks, err := s.renderPostStart(tmpl, data)
	if err != nil {
		return nil, err
	}

	nanoCPUs, memory, err := tmpl.Resources.Limits()
	if err != nil {
		return nil, fmt.Errorf("template %s resources: %w", tmpl.Name, err)
	}

	portMappings, err := s.renderPortBindings(tmpl, data)
	if err != nil {
		return nil, err
	}

	opts := docker.ContainerOptions{
		Name:          containerName,
		Image:         image,
		Cmd:           cmd,
		Env:           envVars,
		NetworkName:   s.traefikNetwork,
		RestartPolicy: docker.RestartUnlessStopped,
		Memory:        memory,
		NanoCPUs:      nanoCPUs,
		PortMappings:  portMappings,
	}

	// Determine the primary HTTP port from the template
	servicePort := 0
	if len(tmpl.Docker.Ports) > 0 {
		servicePort = tmpl.Docker.Ports[0].Container
	}

	// Always mark the container as managed. Traefik is enabled, with a
	// router, only when a domain is set.
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

	return &containerSpec{instanceID: instance.ID, opts: opts, data: data, hooks: hooks, secrets: SecretValues(tmpl, config)}, nil
}

// renderCommand renders each docker.command element. An empty command
// returns nil, which keeps the image's CMD.
func (s *InstanceService) renderCommand(command []string, data map[string]interface{}) ([]string, error) {
	if len(command) == 0 {
		return nil, nil
	}
	cmd := make([]string, 0, len(command))
	for i, arg := range command {
		val, err := s.interpolate(arg, data)
		if err != nil {
			return nil, fmt.Errorf("failed to interpolate command[%d]: %w", i, err)
		}
		cmd = append(cmd, val)
	}
	return cmd, nil
}

// templateData is the data every template string of an instance renders with.
func (s *InstanceService) templateData(tmpl *ServiceTemplate, instance *models.ServiceInstance, config map[string]interface{}) map[string]interface{} {
	domain := ""
	if instance.Domain.Valid {
		domain = instance.Domain.String
	}
	return map[string]interface{}{
		"Config":        templateConfig(tmpl, config),
		"Version":       instance.Version.String,
		"InstanceID":    instance.ID,
		"ContainerName": fmt.Sprintf("goforge-service-%s", instance.ID),
		"Domain":        domain,
		"Protocol":      publicProtocol(domain),
	}
}

// templateConfig returns config with every field the template declares
// present, so a field missing from an older instance's stored config never
// renders as text/template's "<no value>". A missing field takes its declared
// default, or "" when it has none. config is not modified.
func templateConfig(tmpl *ServiceTemplate, config map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(config)+len(tmpl.Config))
	for k, v := range config {
		out[k] = v
	}
	for _, f := range tmpl.Config {
		if v, ok := out[f.Name]; !ok || v == nil {
			if f.Default != nil {
				out[f.Name] = f.Default
			} else {
				out[f.Name] = ""
			}
		}
	}
	return out
}

// renderPostStart renders the template's post_start hooks with data.
func (s *InstanceService) renderPostStart(tmpl *ServiceTemplate, data map[string]interface{}) ([]renderedHook, error) {
	if len(tmpl.PostStart) == 0 {
		return nil, nil
	}
	hooks := make([]renderedHook, 0, len(tmpl.PostStart))
	for i, h := range tmpl.PostStart {
		name := h.Name
		if name == "" {
			name = fmt.Sprintf("post_start[%d]", i)
		}
		cmd := make([]string, 0, len(h.Command))
		for _, arg := range h.Command {
			val, err := s.interpolate(arg, data)
			if err != nil {
				return nil, fmt.Errorf("failed to interpolate post_start %s command: %w", name, err)
			}
			cmd = append(cmd, val)
		}
		keys := make([]string, 0, len(h.Env))
		for k := range h.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		env := make([]string, 0, len(keys))
		for _, k := range keys {
			val, err := s.interpolate(h.Env[k], data)
			if err != nil {
				// The template error names the key, not the rendered value.
				return nil, fmt.Errorf("failed to interpolate post_start %s env %s: %w", name, k, err)
			}
			env = append(env, k+"="+val)
		}
		hooks = append(hooks, renderedHook{name: name, exec: docker.ExecOptions{Cmd: cmd, User: h.User, Env: env}})
	}
	return hooks, nil
}

// errPostStartFailed marks a post_start hook that ran and failed or could not run.
var errPostStartFailed = errors.New("post_start hook failed")

// runPostStartHooks waits for the container to become healthy, then runs each
// hook in order and stops at the first failure. It logs the hook name, exit
// code, and duration. It never logs hook env, and failure output is logged
// only after every secret and hook env value is masked.
func (s *InstanceService) runPostStartHooks(ctx context.Context, dockerID string, spec *containerSpec, logAttrs []any) error {
	redact := hookRedactions(spec)
	s.logger.Info("waiting for service container to become healthy before post_start", append(logAttrs, "hooks", len(spec.hooks))...)
	waitStart := time.Now()
	if err := s.dockerClient.WaitHealthy(ctx, dockerID, s.healthPoll); err != nil {
		s.logger.Error("service container not healthy; post_start skipped", append(logAttrs, "error", RedactSecrets(err.Error(), redact), "waited", time.Since(waitStart).Round(time.Millisecond).String())...)
		return fmt.Errorf("%w: container not healthy: %w", errPostStartFailed, err)
	}

	for _, h := range spec.hooks {
		start := time.Now()
		s.logger.Info("running post_start hook", append(logAttrs, "hook", h.name, "user", h.exec.User)...)
		res, err := s.dockerClient.Exec(ctx, dockerID, h.exec)
		duration := time.Since(start).Round(time.Millisecond).String()
		if err != nil {
			s.logger.Error("post_start hook could not run", append(logAttrs, "hook", h.name, "duration", duration, "error", RedactSecrets(err.Error(), redact))...)
			return fmt.Errorf("%w: %s: %w", errPostStartFailed, h.name, err)
		}
		if res.ExitCode != 0 {
			s.logger.Error("post_start hook failed", append(logAttrs, "hook", h.name, "exit_code", res.ExitCode, "duration", duration, "output", hookOutputTail(RedactSecrets(res.Output, redact)))...)
			return fmt.Errorf("%w: %s exited %d", errPostStartFailed, h.name, res.ExitCode)
		}
		s.logger.Info("post_start hook finished", append(logAttrs, "hook", h.name, "exit_code", 0, "duration", duration)...)
	}
	return nil
}

// hookRedactions returns every value to mask in hook logs: the instance's
// secret config values plus every hook env value, longest first.
func hookRedactions(spec *containerSpec) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, v := range spec.secrets {
		add(v)
	}
	for _, h := range spec.hooks {
		for _, kv := range h.exec.Env {
			if _, v, ok := strings.Cut(kv, "="); ok {
				add(v)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// hookOutputTail keeps the end of hook output, where the error usually is.
func hookOutputTail(out string) string {
	const limit = 2048
	out = strings.TrimSpace(out)
	if len(out) > limit {
		return "..." + out[len(out)-limit:]
	}
	return out
}

// startServiceContainer creates the container described by spec, records it,
// and marks the instance running. When replaced is non-nil its DB row is
// removed once the new container exists.
func (s *InstanceService) startServiceContainer(ctx context.Context, tmpl *ServiceTemplate, spec *containerSpec, replaced *models.Container, logAttrs []any) {
	dockerID, err := s.createServiceContainer(ctx, spec, logAttrs)
	if err != nil {
		s.setInstanceStatus(ctx, spec.instanceID, models.ServiceStatusFailed, lastErrorText("container did not start", err, spec))
		return
	}
	s.finishServiceContainer(ctx, tmpl, spec, dockerID, replaced, logAttrs)
}

// createServiceContainer creates and starts the container for spec. A
// container that was created but could not start (for example, its host port
// is taken) is removed so it does not hold the name.
func (s *InstanceService) createServiceContainer(ctx context.Context, spec *containerSpec, logAttrs []any) (string, error) {
	dockerID, err := s.dockerClient.CreateContainer(ctx, spec.opts)
	if err != nil {
		s.logger.Error("failed to create container", "error", err, "instance_id", spec.instanceID)
		if dockerID != "" {
			if rmErr := s.dockerClient.RemoveContainer(ctx, dockerID); rmErr != nil {
				s.logger.Warn("failed to remove container that did not start", "docker_id", dockerID, "error", rmErr)
			}
		}
		return "", err
	}
	// CreateContainer creates and starts the container in one call.
	s.logger.Info("service container created and started", append(logAttrs, "container", spec.opts.Name, "docker_id", dockerID)...)
	return dockerID, nil
}

// finishServiceContainer records a started container, runs post_start hooks,
// and writes the instance status and connection info.
func (s *InstanceService) finishServiceContainer(ctx context.Context, tmpl *ServiceTemplate, spec *containerSpec, dockerID string, replaced *models.Container, logAttrs []any) {
	instanceID := spec.instanceID
	var err error

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
		Ports:             portMappingsJSON(spec.opts.PortMappings),
	}); err != nil {
		s.logger.Error("failed to record container in DB", "error", err, "instance_id", instanceID)
	}

	status := models.ServiceStatusRunning
	lastErr := ""
	if len(spec.hooks) > 0 {
		if hookErr := s.runPostStartHooks(ctx, dockerID, spec, logAttrs); hookErr != nil {
			status = models.ServiceStatusFailed
			lastErr = lastErrorText("post_start hook failed; container and volumes left in place", hookErr, spec)
		}
	}

	// A hung hook runs until ctx ends. Record the outcome with a fresh
	// deadline so the instance never stays pending.
	statusCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusWriteTimeout)
	defer cancel()

	// Fetch a fresh copy to avoid racing with the returned pointer
	inst, err := s.instanceRepo.GetByID(statusCtx, instanceID)
	if err != nil {
		s.logger.Error("failed to fetch instance for status update", "error", err, "instance_id", instanceID)
		return
	}

	inst.Status = status
	inst.LastError = sql.NullString{String: lastErr, Valid: lastErr != ""}

	// Render connection string templates and store in ConnectionInfo
	if connJSON := s.renderConnectionInfo(tmpl, spec.data, instanceID); connJSON != nil {
		inst.ConnectionInfo = connJSON
	}

	if err = s.instanceRepo.Update(statusCtx, inst); err != nil {
		s.logger.Error("failed to update instance status", "error", err, "instance_id", instanceID, "status", status)
		return
	}
	if status == models.ServiceStatusFailed {
		s.logger.Error("service marked failed: post_start did not complete; container and volumes left in place", logAttrs...)
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
	if genErr := fillGeneratedConfig(tmpl, config, false); genErr != nil {
		return nil, genErr
	}
	// Reject a bad bool or select value while the running container is untouched.
	if cfgErr := normalizeConfig(tmpl, config); cfgErr != nil {
		return nil, cfgErr
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
	if claimErr := s.claimForRecreate(ctx, instance, prevStatus); claimErr != nil {
		return nil, claimErr
	}

	auditID := models.AuditIDFrom(ctx)
	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		defer s.recordRunOutcome(auditID, instance.ID)
		bgCtx, cancel := context.WithTimeout(context.Background(), s.bgTimeout)
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
	if claimErr := s.claimForRecreate(ctx, instance, prevStatus); claimErr != nil {
		return nil, claimErr
	}
	s.logger.Info("service image update requested", "instance_id", instance.ID, "template", tmpl.Name, "image", spec.opts.Image)

	auditID := models.AuditIDFrom(ctx)
	//nolint:contextcheck // Intentional use of background context for async work
	go func() {
		defer s.recordRunOutcome(auditID, instance.ID)
		bgCtx, cancel := context.WithTimeout(context.Background(), s.bgTimeout)
		defer cancel()
		s.recreateContainer(bgCtx, tmpl, spec, pullRequired, prevStatus)
	}()

	return instance, nil
}

// claimForRecreate saves instance (already set to pending) only if its
// stored status is still prevStatus, the status the caller checked. Two
// edits or image updates that both passed the busy check race here; the
// database lets one swap the status, and the other gets ErrInstanceBusy
// before it starts a second recreate (#48).
func (s *InstanceService) claimForRecreate(ctx context.Context, instance *models.ServiceInstance, prevStatus models.ServiceInstanceStatus) error {
	// A new attempt starts; the previous failure no longer describes it.
	instance.LastError = sql.NullString{}
	swapped, err := s.instanceRepo.UpdateIfStatus(ctx, instance, prevStatus)
	if err != nil {
		return fmt.Errorf("failed to save instance: %w", err)
	}
	if !swapped {
		return ErrInstanceBusy
	}
	return nil
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
		// The old container still runs, so the status goes back, but the
		// failed update is still reported.
		s.setInstanceStatus(ctx, spec.instanceID, prevStatus,
			lastErrorText("image pull failed for "+spec.opts.Image+"; existing container left in place", err, spec))
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
		if len(spec.opts.PortMappings) > 0 && s.replaceParked(ctx, tmpl, spec, old, logAttrs) {
			return
		}
		// Docker keeps named volumes on container removal; RemoveVolumes only
		// drops anonymous ones. The goforge-vol-* volumes stay for the new container.
		if removeErr := s.dockerClient.RemoveContainer(ctx, old.DockerID); removeErr != nil {
			s.logger.Warn("failed to remove container", "docker_id", old.DockerID, "error", removeErr)
		}
	}

	s.startServiceContainer(ctx, tmpl, spec, old, logAttrs)
}

// replaceParked swaps the stopped container old for a new one built from a
// spec that publishes host ports. A new container can fail to start when its
// host port is taken, so old is renamed aside instead of removed, and removed
// only once the new container runs. If the new container cannot start, old
// gets its name back and is started again, and the instance is marked failed
// because the edit did not apply. It returns false, having changed nothing,
// when old cannot be renamed; the caller then replaces it the usual way.
func (s *InstanceService) replaceParked(ctx context.Context, tmpl *ServiceTemplate, spec *containerSpec, old *models.Container, logAttrs []any) bool {
	parked := fmt.Sprintf("%s-replaced-%d", spec.opts.Name, time.Now().Unix())
	if err := s.dockerClient.RenameContainer(ctx, old.DockerID, parked); err != nil {
		s.logger.Warn("could not park the old container; removing it before create", append(logAttrs, "docker_id", old.DockerID, "error", err)...)
		return false
	}

	dockerID, err := s.createServiceContainer(ctx, spec, logAttrs)
	if err != nil {
		restored := true
		if renameErr := s.dockerClient.RenameContainer(ctx, old.DockerID, spec.opts.Name); renameErr != nil {
			restored = false
			s.logger.Error("failed to rename the old container back", append(logAttrs, "docker_id", old.DockerID, "parked_as", parked, "error", renameErr)...)
		} else if startErr := s.dockerClient.StartContainer(ctx, old.DockerID); startErr != nil {
			restored = false
			s.logger.Error("failed to restart the old container", append(logAttrs, "docker_id", old.DockerID, "error", startErr)...)
		}
		if restored {
			s.logger.Error("new container did not start; previous container restored and running with the previous configuration", append(logAttrs, "docker_id", old.DockerID, "error", err)...)
		}
		msg := "new container did not start; the edit was not applied"
		if restored {
			msg += " and the previous container is running again"
		}
		s.setInstanceStatus(ctx, spec.instanceID, models.ServiceStatusFailed, lastErrorText(msg, err, spec))
		return true
	}

	// Docker keeps named volumes on container removal; the new container already mounts them.
	if removeErr := s.dockerClient.RemoveContainer(ctx, old.DockerID); removeErr != nil {
		s.logger.Warn("failed to remove the replaced container", append(logAttrs, "docker_id", old.DockerID, "parked_as", parked, "error", removeErr)...)
	}
	s.finishServiceContainer(ctx, tmpl, spec, dockerID, old, logAttrs)
	return true
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
		if field.Name == "name" || field.Name == "domain" || field.Immutable {
			continue
		}
		setString(field.Name, field.Type == FieldTypePassword)
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

// setInstanceStatus fetches the instance and updates its status and
// last_error. lastErr is already redacted (see lastErrorText); empty clears it.
func (s *InstanceService) setInstanceStatus(ctx context.Context, instanceID string, status models.ServiceInstanceStatus, lastErr string) {
	inst, err := s.instanceRepo.GetByID(ctx, instanceID)
	if err != nil {
		s.logger.Error("failed to fetch instance for status update", "error", err, "instance_id", instanceID)
		return
	}
	inst.Status = status
	inst.LastError = sql.NullString{String: lastErr, Valid: lastErr != ""}
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

// templateFuncs are available to every template string. argon2id turns a
// stored secret into the PHC hash a service such as Vaultwarden expects.
var templateFuncs = template.FuncMap{
	"argon2id": argon2idPHC,
}

func (s *InstanceService) interpolate(text string, data interface{}) (string, error) {
	tmpl, err := template.New("service").Funcs(templateFuncs).Parse(text)
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
	instance.LastError = sql.NullString{}
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
	instance.LastError = sql.NullString{}
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

// Template returns the catalog template for name, or nil if unavailable.
func (s *InstanceService) Template(name string) *ServiceTemplate {
	if s == nil || s.catalog == nil {
		return nil
	}
	tmpl, err := s.catalog.GetTemplate(name)
	if err != nil {
		return nil
	}
	return tmpl
}

// maxLastErrorLen caps last_error so a long pull or hook message cannot
// bloat the row or the API answer.
const maxLastErrorLen = 500

// lastErrorText is the last_error stored for a failure: what failed, then
// the error, with every secret config value and hook env value redacted,
// capped at maxLastErrorLen.
func lastErrorText(what string, err error, spec *containerSpec) string {
	msg := what
	if err != nil {
		msg += ": " + err.Error()
	}
	if spec != nil {
		msg = RedactSecrets(msg, hookRedactions(spec))
	}
	msg = strings.TrimSpace(msg)
	if len(msg) > maxLastErrorLen {
		msg = strings.ToValidUTF8(msg[:maxLastErrorLen-3], "") + "..."
	}
	return msg
}
