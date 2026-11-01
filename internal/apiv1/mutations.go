package apiv1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

const maxEnvKeyLen = 128

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// readGate decodes a body that carries only the gate fields.
func readGate(w http.ResponseWriter, r *http.Request) (*gate, bool) {
	g := &gate{}
	if err := decodeBody(r, g); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return nil, false
	}
	return g, true
}

// ---- deployments ----

type triggerRequest struct {
	gate
	EnvID string `json:"env_id,omitempty"`
}

func (h *Handler) triggerDeployment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	req := &triggerRequest{}
	if err := decodeBody(r, req); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if h.deps.Deployments == nil || h.deps.Queue == nil {
		unavailable(w, "deployments")
		return
	}
	ctx := r.Context()
	var env *models.Environment
	if req.EnvID != "" {
		if env, ok = h.loadEnvironment(w, r, p, req.EnvID); !ok {
			return
		}
	} else {
		envs, err := h.deps.Projects.ListEnvironments(ctx, p.ID)
		if err != nil || len(envs) == 0 {
			notFound(w, "environment")
			return
		}
		env = envs[0]
	}
	branch := p.DefaultBranch
	if env.Branch.Valid && env.Branch.String != "" {
		branch = env.Branch.String
	}
	m := &mutation{
		Action:       "trigger_deployment",
		ResourceType: models.ResourceTypeEnvironment,
		ResourceID:   env.ID,
		Target:       map[string]any{"project_id": p.ID, "project": p.Name, "environment_id": env.ID, "environment": env.Name, "branch": branch},
		Plan:         fmt.Sprintf("create a manual deployment of %s (%s) on branch %s and queue it", p.Name, env.Name, branch),
	}
	auditID, ok := h.begin(w, r, &req.gate, m)
	if !ok {
		return
	}
	h.run(w, r, &req.gate, m, auditID, func(ctx context.Context) outcome {
		d := &models.Deployment{EnvironmentID: env.ID, Status: models.DeploymentStatusPending, TriggerType: models.TriggerTypeManual}
		if err := h.deps.Deployments.CreateDeployment(ctx, d); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to create deployment")
		}
		if err := h.deps.Queue.Enqueue(ctx, d.ID); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to queue deployment "+d.ID)
		}
		return succeeded(toDeployment(d))
	})
}

func (h *Handler) cancelDeployment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	d, env, ok := h.loadDeployment(w, r, p)
	if !ok {
		return
	}
	if d.IsTerminal() {
		writeError(w, http.StatusConflict, codeConflict, "deployment is already "+string(d.Status))
		return
	}
	m := &mutation{
		Action:       "cancel_deployment",
		ResourceType: models.ResourceTypeDeployment,
		ResourceID:   d.ID,
		Target:       map[string]any{"project_id": p.ID, "environment_id": env.ID, "deployment_id": d.ID, "status": string(d.Status)},
		Plan:         "cancel deployment " + d.ID + " (" + string(d.Status) + ") and drop it from the queue",
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Deployments.Cancel(ctx, d.ID, "canceled via API"); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to cancel deployment")
		}
		if h.deps.Queue != nil {
			if err := h.deps.Queue.DropPending(ctx, d.ID); err != nil {
				h.logger.Warn("api v1: drop pending failed", "deployment_id", d.ID, "error", err)
			}
		}
		return succeeded(map[string]string{"deployment_id": d.ID, "status": string(models.DeploymentStatusCanceled)})
	})
}

type rollbackRequest struct {
	gate
	Reason string `json:"reason,omitempty"`
}

func (h *Handler) rollbackDeployment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	req := &rollbackRequest{}
	if err := decodeBody(r, req); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	d, env, ok := h.loadDeployment(w, r, p)
	if !ok {
		return
	}
	if h.deps.Rollback == nil {
		unavailable(w, "rollback")
		return
	}
	if !h.deps.Rollback.CanRollback(d) {
		writeError(w, http.StatusConflict, codeConflict, "deployment in status "+string(d.Status)+" cannot be rolled back")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "rollback via API"
	}
	m := &mutation{
		Action:       "rollback_deployment",
		ResourceType: models.ResourceTypeDeployment,
		ResourceID:   d.ID,
		Target:       map[string]any{"project_id": p.ID, "environment_id": env.ID, "environment": env.Name, "deployment_id": d.ID},
		Plan:         "roll environment " + env.Name + " back to its previous successful deployment",
		Changes:      map[string]string{"reason": reason},
	}
	auditID, ok := h.begin(w, r, &req.gate, m)
	if !ok {
		return
	}
	// Rollback stops the current containers and then starts the target. It
	// runs detached from the request so a timeout cannot stop it between
	// the two and leave the environment down.
	h.run(w, r, &req.gate, m, auditID, func(ctx context.Context) outcome {
		info, err := h.deps.Rollback.Rollback(ctx, env.ID, reason)
		if err != nil {
			return failed(err, http.StatusInternalServerError, "rollback failed: "+err.Error())
		}
		return succeeded(map[string]string{
			"from_deployment_id": info.FromDeploymentID,
			"to_deployment_id":   info.ToDeploymentID,
		})
	})
}

// ---- environment variables ----

type setEnvVarRequest struct {
	gate
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

func (h *Handler) setEnvVar(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	req := &setEnvVarRequest{}
	if err := decodeBody(r, req); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	env, ok := h.loadEnvironment(w, r, p, chi.URLParam(r, "envId"))
	if !ok {
		return
	}
	key := strings.TrimSpace(req.Key)
	if len(key) > maxEnvKeyLen || !envKeyPattern.MatchString(key) {
		badRequest(w, "key must match [A-Za-z_][A-Za-z0-9_]* and be at most 128 characters")
		return
	}
	ctx := r.Context()
	vars, err := h.deps.Projects.ListEnvVariables(ctx, env.ID)
	if err != nil {
		internalError(w, "failed to list variables")
		return
	}
	var existing *models.EnvVariable
	for _, v := range vars {
		if v.Key == key {
			existing = v
			break
		}
	}
	op := "create"
	if existing != nil {
		op = "update"
	}
	// The value is write-only: it is never echoed, stored in audit_logs, or logged.
	m := &mutation{
		Action:       "set_env_var",
		ResourceType: models.ResourceTypeEnvironment,
		ResourceID:   env.ID,
		Target:       map[string]any{"project_id": p.ID, "environment_id": env.ID, "environment": env.Name, "key": key},
		Plan:         op + " variable " + key + " in " + env.Name + "; the value takes effect on the next deployment",
		Changes:      map[string]any{"operation": op, "key": key, "is_secret": req.IsSecret, "value": "(write-only)"},
	}
	auditID, ok := h.begin(w, r, &req.gate, m)
	if !ok {
		return
	}
	h.run(w, r, &req.gate, m, auditID, func(ctx context.Context) outcome {
		var err error
		if existing != nil {
			existing.Value = req.Value
			existing.IsSecret = req.IsSecret
			err = h.deps.Projects.UpdateEnvVariable(ctx, existing)
		} else {
			err = h.deps.Projects.AddEnvVariable(ctx, &models.EnvVariable{EnvironmentID: env.ID, Key: key, Value: req.Value, IsSecret: req.IsSecret})
		}
		if err != nil {
			return failed(err, http.StatusInternalServerError, "failed to save variable")
		}
		return succeeded(map[string]any{"key": key, "operation": op, "is_secret": req.IsSecret})
	})
}

func (h *Handler) deleteEnvVar(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	env, ok := h.loadEnvironment(w, r, p, chi.URLParam(r, "envId"))
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	ctx := r.Context()
	vars, err := h.deps.Projects.ListEnvVariables(ctx, env.ID)
	if err != nil {
		internalError(w, "failed to list variables")
		return
	}
	var target *models.EnvVariable
	for _, v := range vars {
		if v.Key == key {
			target = v
			break
		}
	}
	if target == nil {
		notFound(w, "variable")
		return
	}
	m := &mutation{
		Action:       "delete_env_var",
		ResourceType: models.ResourceTypeEnvironment,
		ResourceID:   env.ID,
		Target:       map[string]any{"project_id": p.ID, "environment_id": env.ID, "environment": env.Name, "key": key},
		Plan:         "delete variable " + key + " from " + env.Name + "; running containers keep it until the next deployment",
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Projects.DeleteEnvVariableScoped(ctx, target.ID, env.ID); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to delete variable")
		}
		return succeeded(map[string]string{"key": key, "operation": "delete"})
	})
}

func (h *Handler) syncEnvVars(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	env, ok := h.loadEnvironment(w, r, p, chi.URLParam(r, "envId"))
	if !ok {
		return
	}
	m := &mutation{
		Action:       "sync_env_vars",
		ResourceType: models.ResourceTypeEnvironment,
		ResourceID:   env.ID,
		Target:       map[string]any{"project_id": p.ID, "environment_id": env.ID, "environment": env.Name},
		Plan:         "add keys from the repository's .env.example to " + env.Name + "; existing keys are left alone",
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		n, err := h.deps.Projects.SeedEnvVarsFromRepo(ctx, env.ID, p.ID)
		if err != nil {
			return failed(err, http.StatusBadRequest, "failed to sync variables from the repository")
		}
		return succeeded(map[string]int{"added": n})
	})
}

// ---- service links ----

type linkRequest struct {
	gate
	ServiceInstanceID string `json:"service_instance_id"`
}

func (h *Handler) linkService(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	req := &linkRequest{}
	if err := decodeBody(r, req); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if req.ServiceInstanceID == "" {
		badRequest(w, "service_instance_id is required")
		return
	}
	if h.deps.Links == nil {
		unavailable(w, "service linking")
		return
	}
	inst, ok := h.loadInstance(w, r, req.ServiceInstanceID)
	if !ok {
		return
	}
	ctx := r.Context()
	linkVars, err := services.LinkEnvVars(inst)
	if err != nil {
		linkVars = nil
	}
	keys := make([]string, 0, len(linkVars))
	for k := range linkVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	envs, err := h.deps.Projects.ListEnvironments(ctx, p.ID)
	if err != nil {
		internalError(w, "failed to list environments")
		return
	}
	var env *models.Environment
	if len(envs) > 0 {
		env = envs[0]
	}
	existing := map[string]bool{}
	if env != nil {
		if vars, listErr := h.deps.Projects.ListEnvVariables(ctx, env.ID); listErr == nil {
			for _, v := range vars {
				existing[v.Key] = true
			}
		}
	}
	inject, skip := []string{}, []string{}
	for _, k := range keys {
		if env == nil || existing[k] {
			skip = append(skip, k)
		} else {
			inject = append(inject, k)
		}
	}
	envName := ""
	if env != nil {
		envName = env.Name
	}
	m := &mutation{
		Action:       "link_service",
		ResourceType: models.ResourceTypeProject,
		ResourceID:   p.ID,
		Target:       map[string]any{"project_id": p.ID, "project": p.Name, "service_instance_id": inst.ID, "service": inst.Name},
		Plan:         "link " + inst.Name + " to " + p.Name + " and add its connection variables as secrets to the first environment",
		Changes:      map[string]any{"environment": envName, "inject_keys": inject, "skip_existing_keys": skip},
	}
	auditID, ok := h.begin(w, r, &req.gate, m)
	if !ok {
		return
	}
	h.run(w, r, &req.gate, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Links.Link(ctx, p.ID, inst.ID); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to link service")
		}
		added := []string{}
		for _, k := range inject {
			v := &models.EnvVariable{EnvironmentID: env.ID, Key: k, Value: linkVars[k], IsSecret: true}
			if addErr := h.deps.Projects.AddEnvVariable(ctx, v); addErr != nil {
				h.logger.Warn("api v1: link env var not added", "key", k, "error", addErr)
				continue
			}
			added = append(added, k)
		}
		return succeeded(map[string]any{"linked": true, "added_keys": added})
	})
}

func (h *Handler) unlinkService(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	if h.deps.Links == nil {
		unavailable(w, "service linking")
		return
	}
	serviceID := chi.URLParam(r, "serviceId")
	linked, err := h.deps.Links.ListByProjectID(r.Context(), p.ID)
	if err != nil {
		internalError(w, "failed to list linked services")
		return
	}
	var inst *models.ServiceInstance
	for _, s := range linked {
		if s.ID == serviceID {
			inst = s
			break
		}
	}
	if inst == nil {
		notFound(w, "service link")
		return
	}
	m := &mutation{
		Action:       "unlink_service",
		ResourceType: models.ResourceTypeProject,
		ResourceID:   p.ID,
		Target:       map[string]any{"project_id": p.ID, "project": p.Name, "service_instance_id": inst.ID, "service": inst.Name},
		Plan:         "unlink " + inst.Name + " from " + p.Name + "; injected variables stay and the service keeps running",
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Links.Unlink(ctx, p.ID, inst.ID); err != nil {
			status, msg := http.StatusInternalServerError, "failed to unlink service"
			if errors.Is(err, repositories.ErrNotFound) {
				status, msg = http.StatusNotFound, "service link not found"
			}
			return failed(err, status, msg)
		}
		return succeeded(map[string]bool{"unlinked": true})
	})
}

// ---- service instances ----

type deployServiceRequest struct {
	gate
	Template string         `json:"template"`
	Name     string         `json:"name"`
	Domain   string         `json:"domain,omitempty"`
	Config   map[string]any `json:"config,omitempty"`
}

func (h *Handler) deployService(w http.ResponseWriter, r *http.Request) {
	user, _ := caller(r)
	req := &deployServiceRequest{}
	if err := decodeBody(r, req); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	if h.deps.Instances == nil || h.deps.Catalog == nil {
		unavailable(w, "services")
		return
	}
	tmpl, err := h.deps.Catalog.GetTemplate(req.Template)
	if err != nil || tmpl == nil {
		notFound(w, "service template")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		badRequest(w, "name is required")
		return
	}
	// The catalog form posts strings; keep the same shape.
	config := make(map[string]interface{}, len(req.Config)+2)
	known := map[string]services.ConfigField{}
	for _, f := range tmpl.Config {
		known[f.Name] = f
	}
	for k, v := range req.Config {
		if _, ok := known[k]; !ok {
			badRequest(w, "unknown config field "+k+" for template "+tmpl.Name)
			return
		}
		if v != nil {
			config[k] = fmt.Sprintf("%v", v)
		}
	}
	config["name"] = name
	if d := strings.TrimSpace(req.Domain); d != "" {
		config["domain"] = d
	}

	fields := []map[string]any{}
	for _, f := range tmpl.Config {
		val, set := config[f.Name]
		entry := map[string]any{"field": f.Name, "set": set}
		switch {
		case services.IsSecretField(tmpl, f.Name):
			entry["value"] = "(write-only)"
			if !set && f.Generate {
				entry["value"] = "(generated)"
			}
		case set:
			entry["value"] = val
		case f.Generate:
			entry["value"] = "(generated)"
		default:
			entry["value"] = f.Default
		}
		fields = append(fields, entry)
	}
	m := &mutation{
		Action:       "deploy_service",
		ResourceType: models.ResourceTypeServiceInstance,
		Target:       map[string]any{"template": tmpl.Name, "name": name, "domain": strings.TrimSpace(req.Domain)},
		Plan: fmt.Sprintf("create a new %s instance %q from %s with %d named volume(s); no existing service is changed",
			tmpl.Name, name, tmpl.Docker.Image, len(tmpl.Docker.Volumes)),
		Changes: map[string]any{"fields": fields},
	}
	auditID, ok := h.begin(w, r, &req.gate, m)
	if !ok {
		return
	}
	h.run(w, r, &req.gate, m, auditID, func(ctx context.Context) outcome {
		inst, err := h.deps.Instances.DeployInstance(ctx, user.ID, tmpl.Name, config)
		if err != nil {
			status, msg := http.StatusInternalServerError, "failed to deploy service"
			if errors.Is(err, services.ErrInvalidConfig) {
				status, msg = http.StatusBadRequest, err.Error()
			}
			return failed(err, status, msg)
		}
		return succeeded(h.toServiceSummary(inst))
	})
}

func (h *Handler) restartService(w http.ResponseWriter, r *http.Request) {
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	inst, ok := h.loadInstance(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// Restart keeps the container, config, and volumes, so it is allowed on
	// protected services with only the write scope.
	m := &mutation{
		Action:       "restart_service",
		ResourceType: models.ResourceTypeServiceInstance,
		ResourceID:   inst.ID,
		Target:       h.serviceTarget(inst),
		Plan:         "restart the container of " + inst.Name + " in place (no recreate, config and volumes unchanged)",
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Instances.RestartInstance(ctx, inst.ID); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to restart service")
		}
		return succeeded(map[string]string{"service_instance_id": inst.ID, "status": "restarted"})
	})
}

func (h *Handler) serviceTarget(inst *models.ServiceInstance) map[string]any {
	return map[string]any{
		"service_instance_id": inst.ID,
		"service":             inst.Name,
		"template":            inst.TemplateName,
		"status":              string(inst.Status),
		"protected":           h.isProtected(inst.ID),
	}
}

func (h *Handler) protectedID(instanceID string) string {
	if h.isProtected(instanceID) {
		return instanceID
	}
	return ""
}

func instanceBusy(inst *models.ServiceInstance) bool {
	return inst.Status == models.ServiceStatusPending || inst.Status == models.ServiceStatusStarting
}

func (h *Handler) updateServiceImage(w http.ResponseWriter, r *http.Request) {
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	inst, ok := h.loadInstance(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	tmpl := h.deps.Instances.Template(inst.TemplateName)
	image := ""
	var volumes []map[string]string
	if tmpl != nil {
		image = tmpl.Docker.Image
		volumes = namedVolumes(inst.ID, tmpl)
	}
	m := &mutation{
		Action:       "update_service_image",
		ResourceType: models.ResourceTypeServiceInstance,
		ResourceID:   inst.ID,
		Target:       h.serviceTarget(inst),
		Plan:         "pull " + image + " again and recreate the container of " + inst.Name + " with the same config; named volumes are kept",
		Changes:      map[string]any{"image": image, "keeps_volumes": volumes},
		ProtectedID:  h.protectedID(inst.ID),
	}
	if h.refuseProtected(w, r, m.ProtectedID, m.Action) {
		return
	}
	if instanceBusy(inst) {
		writeError(w, http.StatusConflict, codeConflict, services.ErrInstanceBusy.Error())
		return
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if _, err := h.deps.Instances.UpdateInstanceImage(ctx, inst.ID); err != nil {
			status, msg := http.StatusInternalServerError, "failed to update service image"
			if errors.Is(err, services.ErrInstanceBusy) {
				status, msg = http.StatusConflict, err.Error()
			}
			return failed(err, status, msg)
		}
		return succeeded(map[string]string{"service_instance_id": inst.ID, "status": string(models.ServiceStatusPending)})
	})
}

type updateConfigRequest struct {
	gate
	Config map[string]any `json:"config"`
}

type configChange struct {
	Field  string `json:"field"`
	From   string `json:"from"`
	To     string `json:"to"`
	Secret bool   `json:"secret"`
}

func (h *Handler) updateServiceConfig(w http.ResponseWriter, r *http.Request) {
	req := &updateConfigRequest{}
	if err := decodeBody(r, req); err != nil {
		badRequest(w, "invalid JSON body: "+err.Error())
		return
	}
	inst, ok := h.loadInstance(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	tmpl := h.deps.Instances.Template(inst.TemplateName)
	if tmpl == nil {
		unavailable(w, "service template "+inst.TemplateName)
		return
	}
	pid := h.protectedID(inst.ID)
	// Refuse before computing anything about a protected service.
	if h.refuseProtected(w, r, pid, "update_service_config") {
		return
	}
	if len(req.Config) == 0 {
		badRequest(w, "config must name at least one field")
		return
	}
	current, err := inst.GetConfig()
	if err != nil {
		internalError(w, "failed to read stored config")
		return
	}
	changes, updates, status, msg := diffConfig(tmpl, current, req.Config)
	if status != 0 {
		code := codeBadRequest
		if status == http.StatusUnprocessableEntity {
			code = codeImmutable
		}
		writeError(w, status, code, msg)
		return
	}
	if len(changes) == 0 {
		writeJSON(w, http.StatusOK, mutationResponse{
			Action: "update_service_config", DryRun: req.dryRun(), Target: h.serviceTarget(inst),
			Plan: "no changes; the container is left alone", Changes: changes,
		})
		return
	}
	if instanceBusy(inst) {
		writeError(w, http.StatusConflict, codeConflict, services.ErrInstanceBusy.Error())
		return
	}
	m := &mutation{
		Action:       "update_service_config",
		ResourceType: models.ResourceTypeServiceInstance,
		ResourceID:   inst.ID,
		Target:       h.serviceTarget(inst),
		Plan:         "save the new config and recreate the container of " + inst.Name + "; named volumes are kept",
		Changes:      map[string]any{"fields": changes, "keeps_volumes": namedVolumes(inst.ID, tmpl)},
		ProtectedID:  pid,
	}
	auditID, ok := h.begin(w, r, &req.gate, m)
	if !ok {
		return
	}
	h.run(w, r, &req.gate, m, auditID, func(ctx context.Context) outcome {
		if _, err := h.deps.Instances.UpdateInstanceConfig(ctx, inst.ID, updates); err != nil {
			status, msg := http.StatusInternalServerError, "failed to update service config"
			switch {
			case errors.Is(err, services.ErrInstanceBusy):
				status, msg = http.StatusConflict, err.Error()
			case errors.Is(err, services.ErrInvalidConfig):
				status, msg = http.StatusBadRequest, err.Error()
			}
			return failed(err, status, msg)
		}
		return succeeded(map[string]string{"service_instance_id": inst.ID, "status": string(models.ServiceStatusPending)})
	})
}

// diffConfig validates requested edits against the template and returns the
// secret-free change list plus the update map for UpdateInstanceConfig.
// Unknown fields are 400; a change to an immutable field is 422.
func diffConfig(tmpl *services.ServiceTemplate, current, requested map[string]any) (changes []configChange, updates map[string]interface{}, status int, msg string) {
	fields := map[string]services.ConfigField{}
	for _, f := range tmpl.Config {
		fields[f.Name] = f
	}
	keys := make([]string, 0, len(requested))
	for k := range requested {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	changes = []configChange{}
	updates = map[string]interface{}{}
	for _, k := range keys {
		f, isField := fields[k]
		if !isField && k != "name" && k != "domain" {
			return nil, nil, http.StatusBadRequest, "unknown config field " + k
		}
		to := ""
		if requested[k] != nil {
			to = strings.TrimSpace(fmt.Sprintf("%v", requested[k]))
		}
		from := ""
		if v, ok := current[k]; ok && v != nil {
			from = fmt.Sprintf("%v", v)
		}
		secret := services.IsSecretField(tmpl, k)
		if isField && f.Immutable {
			if to != from {
				return nil, nil, http.StatusUnprocessableEntity, "field " + k + " is immutable and cannot be changed after deploy"
			}
			continue
		}
		// A blank password keeps the stored value, the same as the edit form.
		if secret && to == "" && isField && f.Type == services.FieldTypePassword {
			continue
		}
		if k == "name" && to == "" {
			continue
		}
		if to == from {
			continue
		}
		c := configChange{Field: k, From: from, To: to, Secret: secret}
		if secret {
			c.From, c.To = services.MaskedValue, "(changed, write-only)"
		}
		changes = append(changes, c)
		updates[k] = to
	}
	return changes, updates, 0, ""
}

// ---- containers ----

func (h *Handler) stopContainer(w http.ResponseWriter, r *http.Request) {
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	dockerID, c, ok := h.loadContainer(w, r)
	if !ok {
		return
	}
	if h.deps.Docker == nil {
		unavailable(w, "docker")
		return
	}
	instanceID := containerInstanceID(c)
	m := &mutation{
		Action:       "stop_container",
		ResourceType: "container",
		ResourceID:   dockerID,
		Target:       h.containerTarget(dockerID, c),
		Plan:         "stop container " + containerName(dockerID, c) + "; it stays stopped until started or restarted",
		ProtectedID:  h.protectedID(instanceID),
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Docker.StopContainer(ctx, dockerID); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to stop container")
		}
		if err := h.deps.Containers.UpdateStatus(ctx, c.ID, models.ContainerStatusExited); err != nil {
			h.logger.Warn("api v1: container status not updated", "container_id", c.ID, "error", err)
		}
		return succeeded(map[string]string{"docker_id": dockerID, "status": string(models.ContainerStatusExited)})
	})
}

func (h *Handler) restartContainer(w http.ResponseWriter, r *http.Request) {
	g, ok := readGate(w, r)
	if !ok {
		return
	}
	dockerID, c, ok := h.loadContainer(w, r)
	if !ok {
		return
	}
	if h.deps.Docker == nil {
		unavailable(w, "docker")
		return
	}
	// Restart is allowed on protected services with only the write scope.
	m := &mutation{
		Action:       "restart_container",
		ResourceType: "container",
		ResourceID:   dockerID,
		Target:       h.containerTarget(dockerID, c),
		Plan:         "restart container " + containerName(dockerID, c) + " in place (no recreate)",
	}
	auditID, ok := h.begin(w, r, g, m)
	if !ok {
		return
	}
	h.run(w, r, g, m, auditID, func(ctx context.Context) outcome {
		if err := h.deps.Docker.RestartContainer(ctx, dockerID); err != nil {
			return failed(err, http.StatusInternalServerError, "failed to restart container")
		}
		if err := h.deps.Containers.UpdateStatus(ctx, c.ID, models.ContainerStatusRunning); err != nil {
			h.logger.Warn("api v1: container status not updated", "container_id", c.ID, "error", err)
		}
		return succeeded(map[string]string{"docker_id": dockerID, "status": string(models.ContainerStatusRunning)})
	})
}

func (h *Handler) containerTarget(dockerID string, c *models.Container) map[string]any {
	instanceID := containerInstanceID(c)
	return map[string]any{
		"docker_id":           dockerID,
		"name":                containerName(dockerID, c),
		"service_instance_id": instanceID,
		"protected":           h.isProtected(instanceID),
	}
}

func containerName(dockerID string, c *models.Container) string {
	if c != nil && c.Name != "" {
		return c.Name
	}
	return dockerID
}
