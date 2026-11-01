package apiv1

import (
	"context"
	"database/sql"
	"time"

	"github.com/moby/moby/api/types/container"

	"github.com/raythurman2386/goforge/internal/docker"
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
	ID        string `json:"id"`
	Name      string `json:"name"`
	Template  string `json:"template"`
	Version   string `json:"version,omitempty"`
	Domain    string `json:"domain,omitempty"`
	Status    string `json:"status"`
	Protected bool   `json:"protected"`
	// LastError says why the service last failed (secrets redacted).
	LastError string    `json:"last_error,omitempty"`
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
		Protected: h.instanceProtected(inst),
		LastError: nullString(inst.LastError),
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

// containerStateDTO is the shared secret-free inspect summary; see
// docker.SummarizeInspect. The web UI renders the same struct.
type containerStateDTO = docker.InspectSummary

func toContainerState(info *container.InspectResponse) *containerStateDTO {
	return docker.SummarizeInspect(info)
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

func (h *Handler) toTrackedContainer(ctx context.Context, c *models.Container) trackedContainerDTO {
	instanceID := containerInstanceID(c)
	return trackedContainerDTO{
		DockerID:          c.DockerID,
		Name:              c.Name,
		Image:             c.Image,
		Status:            string(c.Status),
		DeploymentID:      nullString(c.DeploymentID),
		ServiceInstanceID: instanceID,
		Protected:         h.isProtected(ctx, instanceID),
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
