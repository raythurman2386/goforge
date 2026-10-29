package apiv1

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"

	"github.com/raythurman2386/goforge/internal/models"
	"github.com/raythurman2386/goforge/internal/services"
)

func nullString(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func nullTime(t sql.NullTime) *time.Time {
	if t.Valid {
		v := t.Time
		return &v
	}
	return nil
}

type projectDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	RepositoryURL   string    `json:"repository_url"`
	RepositoryName  string    `json:"repository_name,omitempty"`
	GitSourceID     string    `json:"git_source_id,omitempty"`
	DefaultBranch   string    `json:"default_branch"`
	DockerfilePath  string    `json:"dockerfile_path"`
	BuildContext    string    `json:"build_context"`
	HealthCheckPath string    `json:"health_check_path"`
	AutoDeploy      bool      `json:"auto_deploy"`
	CustomDomain    string    `json:"custom_domain,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toProject(p *models.Project) projectDTO {
	return projectDTO{
		ID:              p.ID,
		Name:            p.Name,
		Description:     nullString(p.Description),
		RepositoryURL:   p.RepositoryURL,
		RepositoryName:  nullString(p.RepositoryName),
		GitSourceID:     nullString(p.GitSourceID),
		DefaultBranch:   p.DefaultBranch,
		DockerfilePath:  p.DockerfilePath,
		BuildContext:    p.BuildContext,
		HealthCheckPath: p.HealthCheckPath,
		AutoDeploy:      p.AutoDeploy,
		CustomDomain:    nullString(p.CustomDomain),
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

type environmentDTO struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Domain      string `json:"domain,omitempty"`
	Port        *int32 `json:"port,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Replicas    int    `json:"replicas"`
	CPULimit    string `json:"cpu_limit,omitempty"`
	MemoryLimit string `json:"memory_limit,omitempty"`
	AutoDeploy  bool   `json:"auto_deploy"`
}

func toEnvironment(e *models.Environment) environmentDTO {
	out := environmentDTO{
		ID:          e.ID,
		ProjectID:   e.ProjectID,
		Name:        e.Name,
		Domain:      nullString(e.Domain),
		Branch:      nullString(e.Branch),
		Replicas:    e.Replicas,
		CPULimit:    nullString(e.CPULimit),
		MemoryLimit: nullString(e.MemoryLimit),
		AutoDeploy:  e.AutoDeploy,
	}
	if e.Port.Valid {
		p := e.Port.Int32
		out.Port = &p
	}
	return out
}

// envVarDTO never carries a secret value. Non-secret values are shown as the
// UI shows them; secret values are always masked.
type envVarDTO struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

func toEnvVar(v *models.EnvVariable) envVarDTO {
	value := v.Value
	if v.IsSecret {
		value = services.MaskedValue
	}
	return envVarDTO{ID: v.ID, Key: v.Key, Value: value, IsSecret: v.IsSecret}
}

type deploymentDTO struct {
	ID            string     `json:"id"`
	EnvironmentID string     `json:"environment_id"`
	Status        string     `json:"status"`
	TriggerType   string     `json:"trigger_type"`
	CommitSHA     string     `json:"commit_sha,omitempty"`
	CommitMessage string     `json:"commit_message,omitempty"`
	CommitAuthor  string     `json:"commit_author,omitempty"`
	Branch        string     `json:"branch,omitempty"`
	ImageTag      string     `json:"image_tag,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

func toDeployment(d *models.Deployment) deploymentDTO {
	return deploymentDTO{
		ID:            d.ID,
		EnvironmentID: d.EnvironmentID,
		Status:        string(d.Status),
		TriggerType:   string(d.TriggerType),
		CommitSHA:     nullString(d.CommitSHA),
		CommitMessage: nullString(d.CommitMessage),
		CommitAuthor:  nullString(d.CommitAuthor),
		Branch:        nullString(d.Branch),
		ImageTag:      nullString(d.ImageTag),
		ErrorMessage:  nullString(d.ErrorMessage),
		StartedAt:     nullTime(d.StartedAt),
		FinishedAt:    nullTime(d.FinishedAt),
		CreatedAt:     d.CreatedAt,
	}
}

type serviceSummaryDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Template  string    `json:"template"`
	Version   string    `json:"version,omitempty"`
	Domain    string    `json:"domain,omitempty"`
	Status    string    `json:"status"`
	Protected bool      `json:"protected"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (h *Handler) toServiceSummary(inst *models.ServiceInstance) serviceSummaryDTO {
	return serviceSummaryDTO{
		ID:        inst.ID,
		Name:      inst.Name,
		Template:  inst.TemplateName,
		Version:   nullString(inst.Version),
		Domain:    nullString(inst.Domain),
		Status:    string(inst.Status),
		Protected: h.isProtected(inst.ID),
		CreatedAt: inst.CreatedAt,
		UpdatedAt: inst.UpdatedAt,
	}
}

type configRowDTO struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type templateSummaryDTO struct {
	Name           string   `json:"name"`
	DisplayName    string   `json:"display_name"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	Web            bool     `json:"web"`
	Versions       []string `json:"versions"`
	DefaultVersion string   `json:"default_version"`
}

func toTemplateSummary(t *services.ServiceTemplate) templateSummaryDTO {
	return templateSummaryDTO{
		Name:           t.Name,
		DisplayName:    t.DisplayName,
		Description:    t.Description,
		Category:       t.Category,
		Web:            t.Web,
		Versions:       t.Versions,
		DefaultVersion: t.DefaultVersion,
	}
}

type templateFieldDTO struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Default   any      `json:"default,omitempty"`
	Required  bool     `json:"required"`
	Generate  bool     `json:"generate"`
	Immutable bool     `json:"immutable"`
	Secret    bool     `json:"secret"`
	Length    int      `json:"length,omitempty"`
	Min       int      `json:"min,omitempty"`
	Max       int      `json:"max,omitempty"`
	Options   []string `json:"options,omitempty"`
}

type templatePortDTO struct {
	Container int    `json:"container"`
	Protocol  string `json:"protocol,omitempty"`
	Publish   bool   `json:"publish"`
}

type templateDTO struct {
	templateSummaryDTO
	Image     string                  `json:"image"`
	Fields    []templateFieldDTO      `json:"fields"`
	Ports     []templatePortDTO       `json:"ports"`
	Volumes   []services.VolumeConfig `json:"volumes"`
	Resources services.ResourceConfig `json:"resources"`
}

func toTemplate(t *services.ServiceTemplate) templateDTO {
	out := templateDTO{
		templateSummaryDTO: toTemplateSummary(t),
		Image:              t.Docker.Image,
		Volumes:            t.Docker.Volumes,
		Resources:          t.Resources,
		Fields:             make([]templateFieldDTO, 0, len(t.Config)),
		Ports:              make([]templatePortDTO, 0, len(t.Docker.Ports)),
	}
	for _, f := range t.Config {
		secret := services.IsSecretField(t, f.Name)
		field := templateFieldDTO{
			Name: f.Name, Label: f.Label, Type: f.Type, Required: f.Required,
			Generate: f.Generate, Immutable: f.Immutable, Secret: secret,
			Length: f.Length, Min: f.Min, Max: f.Max, Options: f.Options,
		}
		if !secret {
			field.Default = f.Default
		}
		out.Fields = append(out.Fields, field)
	}
	for _, p := range t.Docker.Ports {
		out.Ports = append(out.Ports, templatePortDTO{Container: p.Container, Protocol: p.Protocol, Publish: p.Publish})
	}
	return out
}

// containerStateDTO is what Docker inspect says about a container, minus
// anything that can hold a secret: the environment, command, and args are
// never copied, and only traefik.enable and router rules are read from labels.
type containerStateDTO struct {
	DockerID      string            `json:"docker_id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	State         string            `json:"state"`
	Running       bool              `json:"running"`
	Health        string            `json:"health,omitempty"`
	RestartCount  int               `json:"restart_count"`
	ExitCode      int               `json:"exit_code"`
	OOMKilled     bool              `json:"oom_killed"`
	StartedAt     string            `json:"started_at,omitempty"`
	FinishedAt    string            `json:"finished_at,omitempty"`
	Ports         []portBindingDTO  `json:"ports"`
	TraefikEnable bool              `json:"traefik_enable"`
	TraefikRules  map[string]string `json:"traefik_rules,omitempty"`
	Volumes       []mountDTO        `json:"volumes"`
	Limits        limitsDTO         `json:"limits"`
	Networks      []string          `json:"networks"`
}

type portBindingDTO struct {
	Container string `json:"container"`
	HostIP    string `json:"host_ip,omitempty"`
	HostPort  string `json:"host_port,omitempty"`
}

type mountDTO struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Destination string `json:"destination"`
	ReadWrite   bool   `json:"read_write"`
}

type limitsDTO struct {
	MemoryBytes int64   `json:"memory_bytes"`
	CPUs        float64 `json:"cpus"`
}

func toContainerState(info *types.ContainerJSON) *containerStateDTO {
	if info == nil || info.ContainerJSONBase == nil {
		return nil
	}
	out := &containerStateDTO{
		DockerID:     info.ID,
		Name:         strings.TrimPrefix(info.Name, "/"),
		RestartCount: info.RestartCount,
		Ports:        []portBindingDTO{},
		Volumes:      []mountDTO{},
		Networks:     []string{},
	}
	if info.Config != nil {
		out.Image = info.Config.Image
		out.TraefikEnable = info.Config.Labels["traefik.enable"] == "true"
		for k, v := range info.Config.Labels {
			if strings.HasPrefix(k, "traefik.http.routers.") && strings.HasSuffix(k, ".rule") {
				if out.TraefikRules == nil {
					out.TraefikRules = map[string]string{}
				}
				out.TraefikRules[k] = v
			}
		}
	}
	if s := info.State; s != nil {
		out.State = s.Status
		out.Running = s.Running
		out.ExitCode = s.ExitCode
		out.OOMKilled = s.OOMKilled
		out.StartedAt = s.StartedAt
		out.FinishedAt = s.FinishedAt
		if s.Health != nil {
			out.Health = s.Health.Status
		}
	}
	if info.HostConfig != nil {
		out.Limits = limitsDTO{MemoryBytes: info.HostConfig.Memory, CPUs: float64(info.HostConfig.NanoCPUs) / 1e9}
	}
	if info.NetworkSettings != nil {
		for port, bindings := range info.NetworkSettings.Ports {
			if len(bindings) == 0 {
				out.Ports = append(out.Ports, portBindingDTO{Container: string(port)})
			}
			for _, b := range bindings {
				out.Ports = append(out.Ports, portBindingDTO{Container: string(port), HostIP: b.HostIP, HostPort: b.HostPort})
			}
		}
		for name := range info.NetworkSettings.Networks {
			out.Networks = append(out.Networks, name)
		}
	}
	sort.Slice(out.Ports, func(i, j int) bool {
		return out.Ports[i].Container+out.Ports[i].HostPort < out.Ports[j].Container+out.Ports[j].HostPort
	})
	sort.Strings(out.Networks)
	for _, m := range info.Mounts {
		out.Volumes = append(out.Volumes, mountDTO{Type: string(m.Type), Name: m.Name, Destination: m.Destination, ReadWrite: m.RW})
	}
	return out
}

type trackedContainerDTO struct {
	DockerID          string             `json:"docker_id"`
	Name              string             `json:"name"`
	Image             string             `json:"image"`
	Status            string             `json:"status"`
	DeploymentID      string             `json:"deployment_id,omitempty"`
	ServiceInstanceID string             `json:"service_instance_id,omitempty"`
	Protected         bool               `json:"protected"`
	CreatedAt         time.Time          `json:"created_at"`
	Docker            *containerStateDTO `json:"docker,omitempty"`
}

func (h *Handler) toTrackedContainer(c *models.Container) trackedContainerDTO {
	instanceID := containerInstanceID(c)
	return trackedContainerDTO{
		DockerID:          c.DockerID,
		Name:              c.Name,
		Image:             c.Image,
		Status:            string(c.Status),
		DeploymentID:      nullString(c.DeploymentID),
		ServiceInstanceID: instanceID,
		Protected:         h.isProtected(instanceID),
		CreatedAt:         c.CreatedAt,
	}
}

type auditDTO struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
	IPAddress    string         `json:"ip_address,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

func toAudit(l *models.AuditLog) auditDTO {
	details, err := l.GetDetails()
	if err != nil {
		details = nil
	}
	return auditDTO{
		ID:           l.ID,
		UserID:       nullString(l.UserID),
		Action:       l.Action,
		ResourceType: l.ResourceType,
		ResourceID:   nullString(l.ResourceID),
		Details:      details,
		IPAddress:    nullString(l.IPAddress),
		CreatedAt:    l.CreatedAt,
	}
}

type gitSourceDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	APIURL    string `json:"api_url,omitempty"`
	IsDefault bool   `json:"is_default"`
	HasToken  bool   `json:"has_token"`
}

func toGitSource(s *models.GitSource) gitSourceDTO {
	return gitSourceDTO{
		ID:        s.ID,
		Name:      s.Name,
		Type:      string(s.Type),
		APIURL:    nullString(s.APIURL),
		IsDefault: s.IsDefault,
		HasToken:  s.AccessToken.Valid && s.AccessToken.String != "",
	}
}
