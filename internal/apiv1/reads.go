package apiv1

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moby/moby/api/pkg/stdcopy"

	"github.com/raythurman2386/goforge/internal/database/repositories"
	"github.com/raythurman2386/goforge/internal/docker"
	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

const (
	defaultListLimit = 50
	maxListLimit     = 500
	defaultLogTail   = 200
	maxLogTail       = 5000
	maxLogBytes      = 2 << 20
	statusOK         = "ok"
	statusError      = "error"
)

// queryInt reads a positive integer query parameter, clamped to max.
func queryInt(r *http.Request, name string, def, maxVal int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	out := map[string]string{"status": statusOK, "version": h.deps.Version}
	if h.deps.DB != nil {
		out["database"] = statusOK
		if err := h.deps.DB.HealthCheck(ctx); err != nil {
			out["database"] = statusError
			out["status"] = "degraded"
		}
	}
	if h.deps.Docker != nil {
		out["docker"] = statusOK
		if err := h.deps.Docker.Ping(ctx); err != nil {
			out["docker"] = statusError
			out["status"] = "degraded"
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) whoami(w http.ResponseWriter, r *http.Request) {
	user, token := caller(r)
	rs := toResourceScope(token)
	scopes := token.Scopes
	if len(scopes) == 0 {
		scopes = []string{models.ScopeFull}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"role":  string(user.Role),
		},
		"token": map[string]any{
			"id":         token.ID,
			"name":       token.Name,
			"scopes":     scopes,
			"expires_at": nullTime(token.ExpiresAt),
			"created_at": token.CreatedAt,
			// Resource restriction (migration 027): both lists empty is
			// host-wide.
			"restricted":  rs.Restricted,
			"project_ids": rs.ProjectIDs,
			"service_ids": rs.ServiceIDs,
		},
		"grants": map[string]bool{
			models.ScopeRead:      token.HasScope(models.ScopeRead),
			models.ScopeWrite:     token.HasScope(models.ScopeWrite),
			models.ScopeProtected: token.HasScope(models.ScopeProtected),
		},
		"protected_services": h.protectedList(r.Context(), user.ID, token),
	})
}

func (h *Handler) dashboardStats(w http.ResponseWriter, r *http.Request) {
	user, _ := caller(r)
	if h.deps.Stats == nil {
		unavailable(w, "stats")
		return
	}
	stats, err := h.deps.Stats.CollectSystemStats(r.Context(), user.ID)
	if err != nil {
		internalError(w, "failed to collect stats")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total_projects":       stats.TotalProjects,
		"total_services":       stats.TotalServices,
		"running_containers":   stats.RunningContainers,
		"total_containers":     stats.TotalContainers,
		"cpu_percent":          stats.TotalCPUUsage,
		"memory_usage_bytes":   stats.TotalMemoryUsage,
		"memory_limit_bytes":   stats.TotalMemoryLimit,
		"memory_usage_percent": stats.MemoryUsagePercent,
		"disk":                 stats.DiskUsage,
	})
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	user, _ := caller(r)
	if h.deps.Projects == nil {
		unavailable(w, "projects")
		return
	}
	projects, err := h.deps.Projects.ListProjects(r.Context(), user.ID)
	if err != nil {
		internalError(w, "failed to list projects")
		return
	}
	out := make([]projectDTO, 0, len(projects))
	for _, p := range projects {
		if !allowsProject(r, p.ID) {
			continue
		}
		out = append(out, toProject(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	envs, err := h.deps.Projects.ListEnvironments(ctx, p.ID)
	if err != nil {
		internalError(w, "failed to list environments")
		return
	}
	type envWithLatest struct {
		environmentDTO
		LatestDeployment *deploymentDTO `json:"latest_deployment,omitempty"`
	}
	envOut := make([]envWithLatest, 0, len(envs))
	for _, e := range envs {
		item := envWithLatest{environmentDTO: toEnvironment(e)}
		if h.deps.DeployList != nil {
			if ds, listErr := h.deps.DeployList.ListByEnvironmentID(ctx, e.ID, 1); listErr == nil && len(ds) > 0 {
				d := toDeployment(ds[0])
				item.LatestDeployment = &d
			}
		}
		envOut = append(envOut, item)
	}
	linked := []serviceSummaryDTO{}
	if h.deps.Links != nil {
		if insts, linkErr := h.deps.Links.ListByProjectID(ctx, p.ID); linkErr == nil {
			for _, inst := range insts {
				linked = append(linked, h.toServiceSummary(inst))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":         toProject(p),
		"environments":    envOut,
		"linked_services": linked,
	})
}

func (h *Handler) listEnvironments(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	envs, err := h.deps.Projects.ListEnvironments(r.Context(), p.ID)
	if err != nil {
		internalError(w, "failed to list environments")
		return
	}
	out := make([]environmentDTO, 0, len(envs))
	for _, e := range envs {
		out = append(out, toEnvironment(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": out})
}

func (h *Handler) listEnvVars(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	env, ok := h.loadEnvironment(w, r, p, chi.URLParam(r, "envId"))
	if !ok {
		return
	}
	vars, err := h.deps.Projects.ListEnvVariables(r.Context(), env.ID)
	if err != nil {
		internalError(w, "failed to list variables")
		return
	}
	out := make([]envVarDTO, 0, len(vars))
	for _, v := range vars {
		out = append(out, toEnvVar(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	writeJSON(w, http.StatusOK, map[string]any{"environment_id": env.ID, "variables": out})
}

func (h *Handler) listDeployments(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	if h.deps.DeployList == nil {
		unavailable(w, "deployments")
		return
	}
	ctx := r.Context()
	limit := queryInt(r, "limit", 20, maxListLimit)

	var envIDs []string
	if envID := r.URL.Query().Get("env_id"); envID != "" {
		env, envOK := h.loadEnvironment(w, r, p, envID)
		if !envOK {
			return
		}
		envIDs = []string{env.ID}
	} else {
		envs, err := h.deps.Projects.ListEnvironments(ctx, p.ID)
		if err != nil {
			internalError(w, "failed to list environments")
			return
		}
		for _, e := range envs {
			envIDs = append(envIDs, e.ID)
		}
	}

	var all []*models.Deployment
	for _, id := range envIDs {
		ds, err := h.deps.DeployList.ListByEnvironmentID(ctx, id, limit)
		if err != nil {
			internalError(w, "failed to list deployments")
			return
		}
		all = append(all, ds...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]deploymentDTO, 0, len(all))
	for _, d := range all {
		out = append(out, toDeployment(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployments": out})
}

func (h *Handler) getDeployment(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	d, env, ok := h.loadDeployment(w, r, p)
	if !ok {
		return
	}
	canRollback := h.deps.Rollback != nil && h.deps.Rollback.CanRollback(d)
	writeJSON(w, http.StatusOK, map[string]any{
		"deployment":   toDeployment(d),
		"environment":  toEnvironment(env),
		"terminal":     d.IsTerminal(),
		"can_rollback": canRollback,
	})
}

func (h *Handler) getDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	p, ok := h.loadProject(w, r)
	if !ok {
		return
	}
	d, _, ok := h.loadDeployment(w, r, p)
	if !ok {
		return
	}
	logs, err := h.deps.Deployments.GetLogs(r.Context(), d.ID)
	if err != nil {
		internalError(w, "failed to read deployment logs")
		return
	}
	tail := queryInt(r, "tail", defaultLogTail, maxLogTail)
	total := len(logs)
	if len(logs) > tail {
		logs = logs[len(logs)-tail:]
	}
	type logLine struct {
		Timestamp time.Time `json:"timestamp"`
		Level     string    `json:"level"`
		Source    string    `json:"source,omitempty"`
		Message   string    `json:"message"`
	}
	out := make([]logLine, 0, len(logs))
	for _, l := range logs {
		out = append(out, logLine{Timestamp: l.Timestamp, Level: string(l.Level), Source: nullString(l.Source), Message: l.Message})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deployment_id": d.ID,
		"status":        string(d.Status),
		"total_lines":   total,
		"lines":         out,
	})
}

func (h *Handler) listServiceTemplates(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Catalog == nil {
		unavailable(w, "service catalog")
		return
	}
	if _, err := h.deps.Catalog.ReloadIfChanged(); err != nil {
		h.logger.Warn("api v1: catalog reload failed", "error", err)
	}
	tmpls := h.deps.Catalog.ListTemplates()
	out := make([]templateSummaryDTO, 0, len(tmpls))
	for _, t := range tmpls {
		out = append(out, toTemplateSummary(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

func (h *Handler) getServiceTemplate(w http.ResponseWriter, r *http.Request) {
	if h.deps.Catalog == nil {
		unavailable(w, "service catalog")
		return
	}
	t, err := h.deps.Catalog.GetTemplate(chi.URLParam(r, "name"))
	if err != nil || t == nil {
		notFound(w, "service template")
		return
	}
	writeJSON(w, http.StatusOK, toTemplate(t))
}

func (h *Handler) listServices(w http.ResponseWriter, r *http.Request) {
	user, _ := caller(r)
	if h.deps.Instances == nil {
		unavailable(w, "services")
		return
	}
	insts, err := h.deps.Instances.ListInstances(r.Context(), user.ID)
	if err != nil {
		internalError(w, "failed to list services")
		return
	}
	out := make([]serviceSummaryDTO, 0, len(insts))
	for _, inst := range insts {
		if !allowsService(r, inst.ID) {
			continue
		}
		out = append(out, h.toServiceSummary(inst))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": out})
}

func (h *Handler) getService(w http.ResponseWriter, r *http.Request) {
	inst, ok := h.loadInstance(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ctx := r.Context()
	tmpl := h.deps.Instances.Template(inst.TemplateName)
	cfgRows, connRows := services.DetailRows(tmpl, inst)
	config := make([]configRowDTO, 0, len(cfgRows))
	for _, row := range cfgRows {
		config = append(config, configRowDTO{Key: row.Key, Value: row.Display, Secret: row.Secret})
	}
	connection := make([]configRowDTO, 0, len(connRows))
	for _, row := range connRows {
		connection = append(connection, configRowDTO{Key: row.Key, Value: row.Display, Secret: row.Secret})
	}

	out := map[string]any{
		"service":    h.toServiceSummary(inst),
		"config":     config,
		"connection": connection,
	}
	if tmpl != nil {
		immutable := []string{}
		for _, f := range tmpl.Config {
			if f.Immutable {
				immutable = append(immutable, f.Name)
			}
		}
		out["immutable_fields"] = immutable
		out["volumes"] = namedVolumes(inst.ID, tmpl)
	}
	if c, err := h.deps.Instances.GetContainer(ctx, inst.ID); err == nil && c != nil {
		tracked := h.toTrackedContainer(ctx, c)
		tracked.Docker = h.inspect(ctx, c.DockerID)
		out["container"] = tracked
	}
	writeJSON(w, http.StatusOK, out)
}

// namedVolumes lists the goforge-vol-<instance>-<name> volumes a recreate keeps.
func namedVolumes(instanceID string, tmpl *services.ServiceTemplate) []map[string]string {
	out := make([]map[string]string, 0, len(tmpl.Docker.Volumes))
	for _, v := range tmpl.Docker.Volumes {
		out = append(out, map[string]string{"name": "goforge-vol-" + instanceID + "-" + v.Name, "path": v.Path})
	}
	return out
}

func (h *Handler) inspect(ctx context.Context, dockerID string) *containerStateDTO {
	if h.deps.Docker == nil || dockerID == "" {
		return nil
	}
	info, err := h.deps.Docker.InspectContainer(ctx, dockerID)
	if err != nil {
		return nil
	}
	return toContainerState(&info)
}

func (h *Handler) listContainers(w http.ResponseWriter, r *http.Request) {
	user, token := caller(r)
	if h.deps.Containers == nil {
		unavailable(w, "containers")
		return
	}
	ctx := r.Context()
	tracked, err := h.deps.Containers.ListByUserID(ctx, user.ID)
	if err != nil {
		internalError(w, "failed to list containers")
		return
	}
	live := map[string]string{}
	dockerOK := false
	if h.deps.Docker != nil {
		if list, listErr := h.deps.Docker.ListContainers(ctx, true); listErr == nil {
			dockerOK = true
			for i := range list {
				live[list[i].ID] = string(list[i].State)
			}
		}
	}
	type item struct {
		trackedContainerDTO
		DockerState string `json:"docker_state,omitempty"`
	}
	out := make([]item, 0, len(tracked))
	for _, c := range tracked {
		if !h.containerInScope(ctx, token, c) {
			continue
		}
		it := item{trackedContainerDTO: h.toTrackedContainer(ctx, c)}
		if dockerOK {
			it.DockerState = live[c.DockerID]
			if it.DockerState == "" {
				it.DockerState = "missing"
			}
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": out, "docker_reachable": dockerOK})
}

func (h *Handler) getContainer(w http.ResponseWriter, r *http.Request) {
	dockerID, c, ok := h.loadContainer(w, r)
	if !ok {
		return
	}
	out := h.toTrackedContainer(r.Context(), c)
	out.Docker = h.inspect(r.Context(), dockerID)
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getContainerLogs(w http.ResponseWriter, r *http.Request) {
	dockerID, c, ok := h.loadContainer(w, r)
	if !ok {
		return
	}
	if h.deps.Docker == nil {
		unavailable(w, "docker")
		return
	}
	tail := queryInt(r, "tail", defaultLogTail, maxLogTail)
	since := strings.TrimSpace(r.URL.Query().Get("since"))
	rc, err := h.deps.Docker.ContainerLogs(r.Context(), dockerID, docker.LogsOptions{Tail: strconv.Itoa(tail), Since: since})
	if err != nil {
		internalError(w, "failed to read container logs")
		return
	}
	defer rc.Close()

	raw, err := io.ReadAll(io.LimitReader(rc, maxLogBytes))
	if err != nil {
		internalError(w, "failed to read container logs")
		return
	}
	text := demuxLogs(raw)
	// A service container's logs can echo its own credentials; mask them the
	// same way the service page masks connection strings.
	if instanceID := containerInstanceID(c); instanceID != "" && h.deps.Instances != nil {
		if inst, instErr := h.deps.Instances.GetInstance(r.Context(), instanceID); instErr == nil && inst != nil {
			if cfg, cfgErr := inst.GetConfig(); cfgErr == nil {
				text = services.RedactSecrets(text, services.SecretValues(h.deps.Instances.Template(inst.TemplateName), cfg))
			}
		}
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"docker_id": dockerID,
		"tail":      tail,
		"since":     since,
		"truncated": len(raw) >= maxLogBytes,
		"lines":     lines,
	})
}

// demuxLogs splits Docker's multiplexed stdout/stderr stream. A TTY
// container's stream is not multiplexed and is returned as is.
func demuxLogs(raw []byte) string {
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, bytes.NewReader(raw)); err != nil {
		return string(raw)
	}
	return stdout.String() + stderr.String()
}

func (h *Handler) getContainerStats(w http.ResponseWriter, r *http.Request) {
	dockerID, _, ok := h.loadContainer(w, r)
	if !ok {
		return
	}
	if h.deps.Stats == nil {
		unavailable(w, "stats")
		return
	}
	m, err := h.deps.Stats.CollectStats(r.Context(), dockerID)
	if err != nil || m == nil {
		internalError(w, "failed to collect container stats")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"docker_id":          dockerID,
		"timestamp":          m.Timestamp,
		"cpu_percent":        m.CPUPercent,
		"memory_usage_bytes": m.MemoryUsage,
		"memory_limit_bytes": m.MemoryLimit,
		"memory_percent":     m.MemoryPercent,
		"network_rx_bytes":   m.NetworkRx,
		"network_tx_bytes":   m.NetworkTx,
		"block_read_bytes":   m.BlockRead,
		"block_write_bytes":  m.BlockWrite,
	})
}

// listAuditLog returns the caller's own audit rows. Admins may pass
// all_users=true to read every user's rows. A restricted token must pass a
// resource_id on its project or service list, and cannot use all_users.
func (h *Handler) listAuditLog(w http.ResponseWriter, r *http.Request) {
	user, token := caller(r)
	if h.deps.Audit == nil {
		unavailable(w, "audit log")
		return
	}
	q := r.URL.Query()
	f := repositories.AuditFilter{
		UserID:       user.ID,
		ResourceType: q.Get("resource_type"),
		ResourceID:   q.Get("resource_id"),
		Action:       q.Get("action"),
		Limit:        queryInt(r, "limit", defaultListLimit, maxListLimit),
		Offset:       queryInt(r, "offset", 0, 1<<30),
	}
	if token.IsRestricted() {
		id := f.ResourceID
		if q.Get("all_users") == "true" || id == "" || !(token.AllowsProject(id) || token.AllowsService(id)) {
			outOfScope(w, "the audit log outside its projects and services (pass resource_id)")
			return
		}
	}
	if q.Get("all_users") == "true" {
		if !user.IsAdmin() {
			forbidden(w, "all_users requires an admin account")
			return
		}
		f.UserID = ""
	}
	logs, err := h.deps.Audit.ListFiltered(r.Context(), f)
	if err != nil {
		internalError(w, "failed to list audit log")
		return
	}
	out := make([]auditDTO, 0, len(logs))
	for _, l := range logs {
		out = append(out, toAudit(l))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "limit": f.Limit, "offset": f.Offset})
}

func (h *Handler) listGitSources(w http.ResponseWriter, r *http.Request) {
	user, _ := caller(r)
	if h.deps.GitSources == nil {
		unavailable(w, "git sources")
		return
	}
	srcs, err := h.deps.GitSources.ListByUserID(r.Context(), user.ID)
	if err != nil {
		internalError(w, "failed to list git sources")
		return
	}
	out := make([]gitSourceDTO, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, toGitSource(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"git_sources": out})
}

func (h *Handler) listRepos(w http.ResponseWriter, r *http.Request) {
	src, ok := h.loadGitSource(w, r)
	if !ok {
		return
	}
	if h.deps.GitBrowser == nil {
		unavailable(w, "git browsing")
		return
	}
	repos, err := h.deps.GitBrowser.ListRepositories(r.Context(), src)
	if err != nil {
		writeError(w, http.StatusBadGateway, codeInternal, "git provider request failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": repos})
}

func (h *Handler) listBranches(w http.ResponseWriter, r *http.Request) {
	src, ok := h.loadGitSource(w, r)
	if !ok {
		return
	}
	if h.deps.GitBrowser == nil {
		unavailable(w, "git browsing")
		return
	}
	repo := r.URL.Query().Get("repo")
	if strings.Count(repo, "/") != 1 {
		badRequest(w, "repo must be owner/name")
		return
	}
	branches, err := h.deps.GitBrowser.ListBranches(r.Context(), src, repo)
	if err != nil {
		writeError(w, http.StatusBadGateway, codeInternal, "git provider request failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repository": repo, "branches": branches})
}
